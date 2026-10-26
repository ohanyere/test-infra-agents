package operational

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessNextEvaluation is safe to run from multiple processes. Completed
// sessions are revisited at a bounded cadence so late evidence creates a new
// immutable revision without duplicating an unchanged report.
func ProcessNextEvaluation(ctx context.Context, pool *pgxpool.Pool, service *Service) (bool, error) {
	_, err := pool.Exec(ctx, `INSERT INTO assurance_evaluation_jobs(session_id,workspace_id,environment_id,run_id) SELECT s.id,s.workspace_id,s.environment_id,s.run_id FROM customer_observation_sessions s JOIN verification_results v ON v.run_id=s.run_id AND v.workspace_id=s.workspace_id WHERE s.status='completed' ON CONFLICT DO NOTHING`)
	if err != nil {
		return false, err
	}
	if _, err = pool.Exec(ctx, `UPDATE assurance_evaluation_jobs SET state='failed',lease_until=NULL,last_error='worker lease expired after maximum attempts',updated_at=now() WHERE state='running' AND lease_until<now() AND attempts>=5`); err != nil {
		return false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var sessionID, w, e string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT session_id,workspace_id,environment_id,attempts FROM assurance_evaluation_jobs WHERE attempts<5 AND next_attempt_at<=now() AND (state IN ('pending','completed') OR (state='running' AND lease_until<now())) ORDER BY next_attempt_at,session_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&sessionID, &w, &e, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	attempts++
	if _, err = tx.Exec(ctx, `UPDATE assurance_evaluation_jobs SET state='running',attempts=$2,lease_until=now()+interval '2 minutes',updated_at=now() WHERE session_id=$1`, sessionID, attempts); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	report, evalErr := service.AssessRunSystem(workCtx, w, e, sessionID)
	if evalErr == nil {
		_, err = pool.Exec(ctx, `UPDATE assurance_evaluation_jobs SET state='completed',attempts=0,lease_until=NULL,next_attempt_at=now()+interval '5 minutes',last_error='',last_fingerprint=$2,updated_at=now() WHERE session_id=$1`, sessionID, report.SourceFingerprint)
		return true, err
	}
	state := "pending"
	if attempts >= 5 {
		state = "failed"
	}
	delay := time.Duration(1<<min(attempts, 5)) * time.Minute
	_, updateErr := pool.Exec(ctx, `UPDATE assurance_evaluation_jobs SET state=$2,attempts=$3,lease_until=NULL,next_attempt_at=now()+$4::interval,last_error=$5,updated_at=now() WHERE session_id=$1`, sessionID, state, attempts, fmt.Sprintf("%d seconds", int(delay.Seconds())), truncateError(evalErr))
	if updateErr != nil {
		return true, updateErr
	}
	return true, evalErr
}
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}
