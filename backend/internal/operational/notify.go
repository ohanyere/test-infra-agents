package operational

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeliverySender interface {
	Send(context.Context, string, []byte) error
}
type WebhookSender struct {
	Endpoint string
	Secret   []byte
	Client   *http.Client
}

func (s WebhookSender) Send(ctx context.Context, id string, payload []byte) error {
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return errors.New("webhook endpoint must use HTTPS")
	}
	if len(s.Secret) < 16 {
		return errors.New("webhook signing secret must be at least 16 bytes")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", id)
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write(payload)
	req.Header.Set("X-Realverax-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("webhook HTTP %d", res.StatusCode)
	}
	return nil
}

// ProcessNextDelivery records success only after the webhook returns 2xx.
// A lease and bounded retry count survive worker process restarts.
func ProcessNextDelivery(ctx context.Context, pool *pgxpool.Pool, sender DeliverySender) (bool, error) {
	if _, err := pool.Exec(ctx, `UPDATE assurance_notification_deliveries SET state='failed',lease_until=NULL,last_error='worker lease expired after maximum attempts' WHERE state='inflight' AND lease_until<now() AND attempts>=5`); err != nil {
		return false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id string
	var payload []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT n.id,n.payload,d.attempts FROM assurance_notification_deliveries d JOIN assurance_notification_events n ON n.id=d.event_id WHERE d.attempts<5 AND d.next_attempt_at<=now() AND (d.state='pending' OR (d.state='inflight' AND d.lease_until<now())) ORDER BY d.next_attempt_at,n.id FOR UPDATE OF d SKIP LOCKED LIMIT 1`).Scan(&id, &payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	attempts++
	if _, err = tx.Exec(ctx, `UPDATE assurance_notification_deliveries SET state='inflight',attempts=$2,lease_until=now()+interval '30 seconds' WHERE event_id=$1`, id, attempts); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	sendErr := sender.Send(deliveryCtx, id, payload)
	if sendErr == nil {
		_, err = pool.Exec(ctx, `UPDATE assurance_notification_deliveries SET state='delivered',lease_until=NULL,delivered_at=now(),last_error='' WHERE event_id=$1 AND state='inflight'`, id)
		return true, err
	}
	state := "pending"
	if attempts >= 5 {
		state = "failed"
	}
	delay := time.Duration(1<<min(attempts, 5)) * time.Minute
	_, err = pool.Exec(ctx, `UPDATE assurance_notification_deliveries SET state=$2,attempts=$3,lease_until=NULL,next_attempt_at=now()+$4::interval,last_error=$5 WHERE event_id=$1 AND state='inflight'`, id, state, attempts, fmt.Sprintf("%d seconds", int(delay.Seconds())), strings.TrimSpace(truncateError(sendErr)))
	if err != nil {
		return true, err
	}
	return true, sendErr
}
