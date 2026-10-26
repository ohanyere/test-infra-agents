// customerauditwebhook is a dedicated direct-mTLS Kubernetes audit receiver.
// It must not be exposed through a TLS-terminating proxy that discards the
// kube-apiserver client certificate.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/database"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

type auditEventList struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Items      []json.RawMessage `json:"items"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("audit webhook stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	addr, certFile, keyFile, dbURL := os.Getenv("AUDIT_WEBHOOK_ADDR"), os.Getenv("AUDIT_WEBHOOK_TLS_CERT"), os.Getenv("AUDIT_WEBHOOK_TLS_KEY"), os.Getenv("DATABASE_URL")
	if addr == "" || certFile == "" || keyFile == "" || dbURL == "" {
		return errors.New("AUDIT_WEBHOOK_ADDR, AUDIT_WEBHOOK_TLS_CERT, AUDIT_WEBHOOK_TLS_KEY, and DATABASE_URL are required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.NewPostgresPool(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	intake := customercollector.NewAuditWebhookIntake(pool)
	server := &http.Server{Addr: addr, Handler: newHandler(intake), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServeTLS(certFile, keyFile) }()
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

type auditIntake interface {
	IngestAuthenticatedAuditEvent(context.Context, *x509.Certificate, []byte) (customercollector.AuthenticatedAuditResult, error)
}

// The handler accepts the Kubernetes audit API path chosen by the webhook
// client. The source is selected by the TLS client certificate, never by URL,
// header, body, forwarded identity, or cluster label.
func newHandler(intake auditIntake) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || !r.TLS.HandshakeComplete {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "audit batch too large", http.StatusRequestEntityTooLarge)
			return
		}
		var batch auditEventList
		if json.Unmarshal(body, &batch) != nil || batch.APIVersion != kubeaudit.AuditAPIVersion || batch.Kind != "EventList" || len(batch.Items) > 400 {
			http.Error(w, "invalid Kubernetes audit EventList", http.StatusBadRequest)
			return
		}
		accepted := 0
		for _, raw := range batch.Items {
			var event kubeaudit.Event
			if len(raw) > 1<<20 || json.Unmarshal(raw, &event) != nil {
				http.Error(w, "invalid audit event", http.StatusBadRequest)
				return
			}
			if _, _, supported := kubeaudit.CustomerActionTarget(event); !supported {
				continue
			}
			if _, err = intake.IngestAuthenticatedAuditEvent(r.Context(), r.TLS.PeerCertificates[0], raw); errors.Is(err, customercollector.ErrNoMatchingAuditSession) {
				continue
			} else if err != nil {
				http.Error(w, "audit event could not be accepted within a unique active session", http.StatusConflict)
				return
			}
			accepted++
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Success", "code": 200, "accepted": accepted})
	})
}
