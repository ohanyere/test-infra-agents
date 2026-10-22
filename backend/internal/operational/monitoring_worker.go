package operational

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MonitorCredentialProvider keeps source credentials outside the database.
// The worker process is the only caller of direct cycle acquisition.
type MonitorCredentialProvider interface {
	Load(context.Context, string) ([]byte, string, error)
}
type FileMonitorCredentials struct{ Directory string }

func (f FileMonitorCredentials) Load(ctx context.Context, id string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if f.Directory == "" {
		return nil, "", errors.New("monitor credential directory is not configured")
	}
	ca, err := os.ReadFile(filepath.Join(f.Directory, id+".ca.pem"))
	if err != nil {
		return nil, "", errors.New("monitor source CA is unavailable")
	}
	token, err := os.ReadFile(filepath.Join(f.Directory, id+".token"))
	if err != nil {
		return nil, "", errors.New("monitor source token is unavailable")
	}
	value := strings.TrimSpace(string(token))
	if value == "" {
		return nil, "", errors.New("monitor source token is empty")
	}
	return ca, value, nil
}

// ProcessNextMonitoring claims one durable cycle with SKIP LOCKED. A lease
// recovery reuses its cycle ID; each source acquisition is idempotent within
// that cycle. At most six bounded HTTPS requests run sequentially. The optional
// external vantage is set only in the separately operated probe deployment.
func ProcessNextMonitoring(ctx context.Context, pool *pgxpool.Pool, service *Service, credentials MonitorCredentialProvider, probeVantage ...string) (bool, error) {
	_, err := pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET state='completed',updated_at=now() WHERE state='active' AND current_cycle_id IS NULL AND ends_at<=now()`)
	if err != nil {
		return false, err
	}
	_, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET state='failed',lease_until=NULL,last_error='monitor lease expired after maximum attempts',updated_at=now() WHERE state='running' AND lease_until<now() AND attempts>=5`)
	if err != nil {
		return false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var session, w, e, cycle string
	var number, attempt, cadence int
	var ends time.Time
	err = tx.QueryRow(ctx, `SELECT session_id,workspace_id,environment_id,COALESCE(current_cycle_id::text,''),cycle_number,attempts,cadence_seconds,ends_at FROM assurance_monitoring_plans WHERE next_due_at<=now() AND attempts<5 AND (state='active' AND (ends_at>now() OR current_cycle_id IS NOT NULL) OR state='running' AND lease_until<now()) ORDER BY next_due_at,session_id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&session, &w, &e, &cycle, &number, &attempt, &cadence, &ends)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if cycle == "" {
		cycle = newCycleID()
		number++
	}
	attempt++
	if number > 288 {
		if _, err = tx.Exec(ctx, `UPDATE assurance_monitoring_plans SET state='failed',lease_until=NULL,last_error='monitor cycle limit exceeded',updated_at=now() WHERE session_id=$1`, session); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE assurance_monitoring_plans SET state='running',current_cycle_id=$2,cycle_number=$3,attempts=$4,lease_until=now()+interval '2 minutes',updated_at=now() WHERE session_id=$1`, session, cycle, number, attempt); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	rows, err := pool.Query(workCtx, `SELECT ps.source_id,o.source_kind FROM assurance_monitoring_plan_sources ps JOIN customer_outcome_sources o ON o.id=ps.source_id AND o.workspace_id=$2 AND o.environment_id=$3 WHERE ps.session_id=$1 ORDER BY (o.source_kind='external_reachability'),ps.source_id`, session, w, e)
	if err != nil {
		return true, err
	}
	type scheduledSource struct{ id, kind string }
	sources := []scheduledSource{}
	for rows.Next() {
		var source scheduledSource
		if err = rows.Scan(&source.id, &source.kind); err != nil {
			rows.Close()
			return true, err
		}
		sources = append(sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	var failure error
	for _, source := range sources {
		var observation DirectObservation
		var loadErr error
		if source.kind == "external_reachability" {
			vantage := ""
			if len(probeVantage) == 1 {
				vantage = probeVantage[0]
			}
			observation, loadErr = service.AcquireReachabilityCycle(workCtx, source.id, session, cycle, vantage)
		} else {
			var ca []byte
			var token string
			ca, token, loadErr = credentials.Load(workCtx, source.id)
			if loadErr == nil {
				observation, loadErr = service.AcquireDirectCycle(workCtx, source.id, session, cycle, ca, token)
			}
		}
		status, reason, observationID := "acquired", "", observation.ID
		if loadErr != nil {
			status = "unavailable"
			reason = truncateError(loadErr)
			observationID = ""
			if failure == nil {
				failure = loadErr
			}
		}
		_, err = pool.Exec(ctx, `INSERT INTO assurance_monitoring_events(workspace_id,environment_id,session_id,cycle_id,cycle_number,attempt,source_id,status,observation_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,$10) ON CONFLICT(cycle_id,source_id,attempt) DO NOTHING`, w, e, session, cycle, number, attempt, source.id, status, observationID, reason)
		if err != nil {
			return true, err
		}
	}
	// An acquisition failure is source-health evidence, never service-health
	// evidence. Assessment reads the events and preserves an append-only gap.
	_, assessmentErr := service.AssessRunSystem(workCtx, w, e, session)
	if assessmentErr != nil && failure == nil {
		failure = assessmentErr
	}
	if failure == nil {
		state := "active"
		next := time.Now().UTC().Add(time.Duration(cadence) * time.Second)
		if !next.Before(ends) {
			state = "completed"
		}
		_, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET state=$3,attempts=0,current_cycle_id=NULL,lease_until=NULL,next_due_at=$4,last_error='',last_success_at=now(),updated_at=now() WHERE session_id=$1 AND current_cycle_id=$2 AND state='running'`, session, cycle, state, next)
		return true, err
	}
	state := "active"
	if attempt >= 5 {
		state = "failed"
	}
	delay := time.Duration(1<<min(attempt, 5)) * time.Minute
	_, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET state=$3,lease_until=NULL,next_due_at=now()+$4::interval,last_error=$5,updated_at=now() WHERE session_id=$1 AND current_cycle_id=$2 AND state='running'`, session, cycle, state, fmt.Sprintf("%d seconds", int(delay.Seconds())), truncateError(failure))
	if err != nil {
		return true, err
	}
	return true, failure
}
