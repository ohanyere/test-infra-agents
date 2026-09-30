package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

func (r *Repository) LockBatch(ctx context.Context, db DBTX, workspaceID, batchID string) (BatchState, int, error) {
	var out BatchState
	var runCount int
	err := db.QueryRow(ctx, `SELECT b.id,b.workspace_id,b.status,b.created_at,b.started_at,b.completed_at,(SELECT count(*) FROM runs r WHERE r.batch_id=b.id) FROM evaluation_batches b WHERE b.id=$1 AND b.workspace_id=$2 FOR UPDATE`, batchID, workspaceID).Scan(&out.ID, &out.WorkspaceID, &out.Status, &out.CreatedAt, &out.StartedAt, &out.CompletedAt, &runCount)
	return out, runCount, mapDB(err)
}

func (r *Repository) QueueBatch(ctx context.Context, db DBTX, batchID string) (BatchState, error) {
	var out BatchState
	err := db.QueryRow(ctx, `UPDATE evaluation_batches SET status='queued' WHERE id=$1 RETURNING id,workspace_id,status,created_at,started_at,completed_at`, batchID).Scan(&out.ID, &out.WorkspaceID, &out.Status, &out.CreatedAt, &out.StartedAt, &out.CompletedAt)
	return out, mapDB(err)
}

func (r *Repository) StopQueuedBatch(ctx context.Context, db DBTX, batchID string) error {
	_, err := db.Exec(ctx, `UPDATE runs SET status='stopped',completed_at=COALESCE(completed_at,now()) WHERE batch_id=$1 AND status='queued'`, batchID)
	return mapDB(err)
}

func (r *Repository) StopBatch(ctx context.Context, db DBTX, batchID string) (BatchState, error) {
	var out BatchState
	err := db.QueryRow(ctx, `UPDATE evaluation_batches SET status='stopped',completed_at=COALESCE(completed_at,now()) WHERE id=$1 RETURNING id,workspace_id,status,created_at,started_at,completed_at`, batchID).Scan(&out.ID, &out.WorkspaceID, &out.Status, &out.CreatedAt, &out.StartedAt, &out.CompletedAt)
	return out, mapDB(err)
}

func (r *Repository) LockNextQueuedRun(ctx context.Context, db DBTX) (ExecutionRun, error) {
	var out ExecutionRun
	err := db.QueryRow(ctx, `SELECT r.id,b.id,b.workspace_id,r.attempt_number,s.id,sv.id,sv.version,a.id,av.id,av.version_label,av.interface_kind,av.capabilities,av.metadata,r.agent_implementation_identity,b.environment_mode,b.config,COALESCE(rv.variant_id,''),COALESCE(rv.variant_seed,0),COALESCE(rv.hidden_config,'{}'::jsonb) FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN scenario_versions sv ON sv.id=b.scenario_version_id JOIN scenarios s ON s.id=sv.scenario_id JOIN agent_versions av ON av.id=r.agent_version_id JOIN agents a ON a.id=av.agent_id LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id WHERE r.status='queued' AND b.status IN ('queued','running') ORDER BY b.created_at ASC,b.id ASC,r.created_at ASC,r.id ASC FOR UPDATE OF r SKIP LOCKED LIMIT 1`).Scan(&out.RunID, &out.BatchID, &out.WorkspaceID, &out.AttemptNumber, &out.Scenario.ID, &out.Scenario.VersionID, &out.Scenario.Version, &out.Agent.ID, &out.Agent.VersionID, &out.Agent.VersionLabel, &out.Agent.InterfaceKind, &out.Agent.Capabilities, &out.Agent.Metadata, &out.Agent.ImplementationIdentity, &out.Batch.EnvironmentMode, &out.Batch.Config, &out.HiddenVariant.ID, &out.HiddenVariant.Seed, &out.HiddenVariant.Config)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNoRunAvailable
	}
	return out, mapDB(err)
}

func (r *Repository) ClaimRun(ctx context.Context, db DBTX, runID string) error {
	_, err := db.Exec(ctx, `UPDATE runs SET status='provisioning',started_at=COALESCE(started_at,now()) WHERE id=$1 AND status='queued'`, runID)
	return mapDB(err)
}

func (r *Repository) ClaimRunWithLease(ctx context.Context, db DBTX, runID, workerID string, leaseMilliseconds int64) error {
	tag, err := db.Exec(ctx, `UPDATE runs SET status='provisioning',started_at=COALESCE(started_at,now()),worker_id=$2,heartbeat_at=now(),lease_expires_at=now()+($3*interval '1 millisecond') WHERE id=$1 AND status='queued'`, runID, workerID, leaseMilliseconds)
	if err == nil && tag.RowsAffected() != 1 {
		return identity.ErrConflict
	}
	return mapDB(err)
}

func (r *Repository) RenewRunLease(ctx context.Context, db DBTX, workspaceID, runID, workerID string, leaseMilliseconds int64) (bool, error) {
	tag, err := db.Exec(ctx, `UPDATE runs r SET heartbeat_at=now(),lease_expires_at=now()+($4*interval '1 millisecond') FROM evaluation_batches b WHERE r.id=$1 AND r.batch_id=b.id AND b.workspace_id=$2 AND r.worker_id=$3 AND r.status IN ('provisioning','running') AND r.lease_expires_at>now()`, runID, workspaceID, workerID, leaseMilliseconds)
	return tag.RowsAffected() == 1, mapDB(err)
}

func (r *Repository) VerifyRunLease(ctx context.Context, db DBTX, workspaceID, runID, workerID string) (bool, error) {
	var owned bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2 AND r.worker_id=$3 AND r.status IN ('provisioning','running') AND r.lease_expires_at>now())`, runID, workspaceID, workerID).Scan(&owned)
	return owned, mapDB(err)
}

func (r *Repository) LockNextAbandonedRun(ctx context.Context, db DBTX, graceMilliseconds int64) (AbandonedRun, error) {
	var out AbandonedRun
	err := db.QueryRow(ctx, `SELECT r.id,b.id,b.workspace_id,r.attempt_number,s.id,sv.id,sv.version,a.id,av.id,av.version_label,av.interface_kind,av.capabilities,av.metadata,r.agent_implementation_identity,b.environment_mode,b.config,COALESCE(rv.variant_id,''),COALESCE(rv.variant_seed,0),COALESCE(rv.hidden_config,'{}'::jsonb),r.status,r.environment_ref FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN scenario_versions sv ON sv.id=b.scenario_version_id JOIN scenarios s ON s.id=sv.scenario_id JOIN agent_versions av ON av.id=r.agent_version_id JOIN agents a ON a.id=av.agent_id LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id WHERE r.status IN ('provisioning','running') AND r.lease_expires_at IS NOT NULL AND r.lease_expires_at < now()-($1*interval '1 millisecond') ORDER BY r.lease_expires_at,r.id FOR UPDATE OF r SKIP LOCKED LIMIT 1`, graceMilliseconds).Scan(&out.RunID, &out.BatchID, &out.WorkspaceID, &out.AttemptNumber, &out.Scenario.ID, &out.Scenario.VersionID, &out.Scenario.Version, &out.Agent.ID, &out.Agent.VersionID, &out.Agent.VersionLabel, &out.Agent.InterfaceKind, &out.Agent.Capabilities, &out.Agent.Metadata, &out.Agent.ImplementationIdentity, &out.Batch.EnvironmentMode, &out.Batch.Config, &out.HiddenVariant.ID, &out.HiddenVariant.Seed, &out.HiddenVariant.Config, &out.PreviousStatus, &out.EnvironmentRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNoRunAvailable
	}
	return out, mapDB(err)
}

func (r *Repository) TakeOverRunLease(ctx context.Context, db DBTX, runID, workerID string, leaseMilliseconds, graceMilliseconds int64) error {
	tag, err := db.Exec(ctx, `UPDATE runs SET worker_id=$2,heartbeat_at=now(),lease_expires_at=now()+($3*interval '1 millisecond') WHERE id=$1 AND status IN ('provisioning','running') AND lease_expires_at IS NOT NULL AND lease_expires_at < now()-($4*interval '1 millisecond')`, runID, workerID, leaseMilliseconds, graceMilliseconds)
	if err == nil && tag.RowsAffected() != 1 {
		return identity.ErrConflict
	}
	return mapDB(err)
}

func (r *Repository) StartBatch(ctx context.Context, db DBTX, batchID string) error {
	_, err := db.Exec(ctx, `UPDATE evaluation_batches SET status='running',started_at=COALESCE(started_at,now()) WHERE id=$1 AND status IN ('queued','running')`, batchID)
	return mapDB(err)
}

func (r *Repository) LockRun(ctx context.Context, db DBTX, runID string) (string, string, error) {
	var status, batchID string
	err := db.QueryRow(ctx, `SELECT status,batch_id FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&status, &batchID)
	return status, batchID, mapDB(err)
}

func (r *Repository) LockOwnedRun(ctx context.Context, db DBTX, runID, workerID string) (string, string, error) {
	var status, batchID string
	err := db.QueryRow(ctx, `SELECT status,batch_id FROM runs WHERE id=$1 AND worker_id=$2 AND lease_expires_at>now() FOR UPDATE`, runID, workerID).Scan(&status, &batchID)
	return status, batchID, mapDB(err)
}

func (r *Repository) LockBatchByID(ctx context.Context, db DBTX, batchID string) error {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM evaluation_batches WHERE id=$1 FOR UPDATE`, batchID).Scan(&id)
	return mapDB(err)
}

func (r *Repository) MarkRunRunning(ctx context.Context, db DBTX, runID, environmentRef string) error {
	_, err := db.Exec(ctx, `UPDATE runs SET status='running',environment_ref=$2 WHERE id=$1`, runID, environmentRef)
	return mapDB(err)
}

func (r *Repository) MarkOwnedRunRunning(ctx context.Context, db DBTX, runID, workerID, environmentRef string) error {
	tag, err := db.Exec(ctx, `UPDATE runs SET status='running',environment_ref=$3 WHERE id=$1 AND worker_id=$2 AND status='provisioning' AND lease_expires_at>now()`, runID, workerID, environmentRef)
	if err == nil && tag.RowsAffected() != 1 {
		return identity.ErrConflict
	}
	return mapDB(err)
}

func (r *Repository) MarkRunTerminal(ctx context.Context, db DBTX, runID, status string) error {
	_, err := db.Exec(ctx, `UPDATE runs SET status=$2,completed_at=COALESCE(completed_at,now()),worker_id=NULL,lease_expires_at=NULL WHERE id=$1`, runID, status)
	return mapDB(err)
}

func (r *Repository) MarkOwnedRunTerminal(ctx context.Context, db DBTX, runID, workerID, status string) error {
	tag, err := db.Exec(ctx, `UPDATE runs SET status=$3,completed_at=COALESCE(completed_at,now()),worker_id=NULL,lease_expires_at=NULL WHERE id=$1 AND worker_id=$2 AND lease_expires_at>now() AND status IN ('provisioning','running')`, runID, workerID, status)
	if err == nil && tag.RowsAffected() != 1 {
		return identity.ErrConflict
	}
	return mapDB(err)
}

func (r *Repository) ReconcileBatch(ctx context.Context, db DBTX, batchID string) error {
	var total, queued, active, completed, failed, stopped int
	err := db.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status='queued'),count(*) FILTER (WHERE status IN ('provisioning','running')),count(*) FILTER (WHERE status='completed'),count(*) FILTER (WHERE status='failed'),count(*) FILTER (WHERE status='stopped') FROM runs WHERE batch_id=$1`, batchID).Scan(&total, &queued, &active, &completed, &failed, &stopped)
	if err != nil {
		return mapDB(err)
	}
	status := "running"
	terminal := false
	switch {
	case total > 0 && completed == total:
		status, terminal = "completed", true
	case total > 0 && completed+failed+stopped == total && failed > 0:
		status, terminal = "failed", true
	case total > 0 && completed+stopped == total && stopped > 0:
		status, terminal = "stopped", true
	case active > 0 || queued > 0:
		status = "running"
	}
	if terminal {
		_, err = db.Exec(ctx, `UPDATE evaluation_batches SET status=$2,completed_at=COALESCE(completed_at,now()) WHERE id=$1 AND status!='stopped'`, batchID, status)
	} else {
		_, err = db.Exec(ctx, `UPDATE evaluation_batches SET status=$2 WHERE id=$1 AND status!='stopped'`, batchID, status)
	}
	return mapDB(err)
}

func mapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return identity.ErrConflict
	}
	return fmt.Errorf("execution database operation: %w", err)
}
