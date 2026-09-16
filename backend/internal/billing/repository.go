package billing

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX abstracts pgx pool/tx for testability.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository owns billing SQL. Ledger rows are append-only; no UPDATE/DELETE
// paths exist here by design.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

func (r *Repository) GetAccount(ctx context.Context, db DBTX, workspaceID string) (BillingAccount, error) {
	var out BillingAccount
	err := db.QueryRow(ctx, `SELECT workspace_id, plan_id, status, period_start, period_end,
		catalog_version, COALESCE(provider,''), COALESCE(customer_ref,''), COALESCE(subscription_ref,''),
		created_at, updated_at
		FROM billing_accounts WHERE workspace_id=$1`, workspaceID).Scan(
		&out.WorkspaceID, &out.PlanID, &out.Status, &out.PeriodStart, &out.PeriodEnd,
		&out.CatalogVersion, &out.Provider, &out.CustomerRef, &out.SubscriptionRef,
		&out.CreatedAt, &out.UpdatedAt)
	return out, mapDB(err)
}

func (r *Repository) LockAccount(ctx context.Context, db DBTX, workspaceID string) (BillingAccount, error) {
	var out BillingAccount
	err := db.QueryRow(ctx, `SELECT workspace_id, plan_id, status, period_start, period_end,
		catalog_version, COALESCE(provider,''), COALESCE(customer_ref,''), COALESCE(subscription_ref,''),
		created_at, updated_at
		FROM billing_accounts WHERE workspace_id=$1 FOR UPDATE`, workspaceID).Scan(
		&out.WorkspaceID, &out.PlanID, &out.Status, &out.PeriodStart, &out.PeriodEnd,
		&out.CatalogVersion, &out.Provider, &out.CustomerRef, &out.SubscriptionRef,
		&out.CreatedAt, &out.UpdatedAt)
	return out, mapDB(err)
}

func (r *Repository) CreateAccount(ctx context.Context, db DBTX, workspaceID, planID, status string, periodStart, periodEnd time.Time) (BillingAccount, error) {
	var out BillingAccount
	err := db.QueryRow(ctx, `INSERT INTO billing_accounts(workspace_id, plan_id, status, period_start, period_end, catalog_version)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING workspace_id, plan_id, status, period_start, period_end, catalog_version,
			COALESCE(provider,''), COALESCE(customer_ref,''), COALESCE(subscription_ref,''),
			created_at, updated_at`,
		workspaceID, planID, status, periodStart, periodEnd, BillingCatalogContractVersion).Scan(
		&out.WorkspaceID, &out.PlanID, &out.Status, &out.PeriodStart, &out.PeriodEnd,
		&out.CatalogVersion, &out.Provider, &out.CustomerRef, &out.SubscriptionRef,
		&out.CreatedAt, &out.UpdatedAt)
	return out, mapDB(err)
}

func (r *Repository) UpdatePeriod(ctx context.Context, db DBTX, workspaceID string, periodStart, periodEnd time.Time) error {
	_, err := db.Exec(ctx, `UPDATE billing_accounts SET period_start=$2, period_end=$3, updated_at=now()
		WHERE workspace_id=$1`, workspaceID, periodStart, periodEnd)
	return mapDB(err)
}

func (r *Repository) UpdateSubscription(ctx context.Context, db DBTX, workspaceID, planID, status, provider, customerRef, subscriptionRef string, periodStart, periodEnd time.Time) error {
	_, err := db.Exec(ctx, `UPDATE billing_accounts
		SET plan_id=$2, status=$3, provider=$4, customer_ref=NULLIF($5,''), subscription_ref=NULLIF($6,''),
		    period_start=$7, period_end=$8, catalog_version=$9, updated_at=now()
		WHERE workspace_id=$1`, workspaceID, planID, status, provider, customerRef, subscriptionRef, periodStart, periodEnd, BillingCatalogContractVersion)
	return mapDB(err)
}

// InsertEntry appends one ledger row. Duplicate idempotency keys map to
// identity.ErrConflict so callers can treat repeats as no-ops.
func (r *Repository) InsertEntry(ctx context.Context, db DBTX, in UsageEntry) (UsageEntry, error) {
	var out UsageEntry
	err := db.QueryRow(ctx, `INSERT INTO billing_usage_ledger(
		workspace_id, kind, quantity, meter_contract, catalog_contract, idempotency_key,
		suite_execution_id, run_id, batch_id, scenario_version_id,
		environment_profile, provider, isolation_tier, reason, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, workspace_id, kind, quantity, meter_contract, catalog_contract, idempotency_key,
			suite_execution_id, run_id, batch_id, scenario_version_id,
			environment_profile, provider, isolation_tier, reason, created_by, created_at`,
		in.WorkspaceID, in.Kind, in.Quantity, in.MeterContract, in.CatalogContract, in.IdempotencyKey,
		in.SuiteExecutionID, in.RunID, in.BatchID, in.ScenarioVersionID,
		in.EnvironmentProfile, in.Provider, in.IsolationTier, in.Reason, in.CreatedBy).Scan(
		&out.ID, &out.WorkspaceID, &out.Kind, &out.Quantity, &out.MeterContract, &out.CatalogContract, &out.IdempotencyKey,
		&out.SuiteExecutionID, &out.RunID, &out.BatchID, &out.ScenarioVersionID,
		&out.EnvironmentProfile, &out.Provider, &out.IsolationTier, &out.Reason, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) GetEntryByIdempotency(ctx context.Context, db DBTX, workspaceID, idempotencyKey string) (UsageEntry, error) {
	var out UsageEntry
	err := db.QueryRow(ctx, `SELECT id, workspace_id, kind, quantity, meter_contract, catalog_contract, idempotency_key,
		suite_execution_id, run_id, batch_id, scenario_version_id,
		environment_profile, provider, isolation_tier, reason, created_by, created_at
		FROM billing_usage_ledger WHERE workspace_id=$1 AND idempotency_key=$2`,
		workspaceID, idempotencyKey).Scan(
		&out.ID, &out.WorkspaceID, &out.Kind, &out.Quantity, &out.MeterContract, &out.CatalogContract, &out.IdempotencyKey,
		&out.SuiteExecutionID, &out.RunID, &out.BatchID, &out.ScenarioVersionID,
		&out.EnvironmentProfile, &out.Provider, &out.IsolationTier, &out.Reason, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

// SumByKind returns net quantities grouped by kind for the current period.
func (r *Repository) SumByKind(ctx context.Context, db DBTX, workspaceID string, since time.Time) (map[string]int64, error) {
	rows, err := db.Query(ctx, `SELECT kind, COALESCE(SUM(quantity),0)
		FROM billing_usage_ledger WHERE workspace_id=$1 AND created_at >= $2 GROUP BY kind`,
		workspaceID, since)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var kind string
		var total int64
		if err := rows.Scan(&kind, &total); err != nil {
			return nil, err
		}
		out[kind] = total
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) RecentEntries(ctx context.Context, db DBTX, workspaceID string, limit int) ([]UsageEntry, error) {
	rows, err := db.Query(ctx, `SELECT id, workspace_id, kind, quantity, meter_contract, catalog_contract, idempotency_key,
		suite_execution_id, run_id, batch_id, scenario_version_id,
		environment_profile, provider, isolation_tier, reason, created_by, created_at
		FROM billing_usage_ledger WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []UsageEntry{}
	for rows.Next() {
		var e UsageEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Kind, &e.Quantity, &e.MeterContract, &e.CatalogContract, &e.IdempotencyKey,
			&e.SuiteExecutionID, &e.RunID, &e.BatchID, &e.ScenarioVersionID,
			&e.EnvironmentProfile, &e.Provider, &e.IsolationTier, &e.Reason, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) InsertProviderEvent(ctx context.Context, db DBTX, provider, providerEventID, eventType, workspaceID string, payload string) (ProviderEvent, error) {
	var out ProviderEvent
	err := db.QueryRow(ctx, `INSERT INTO billing_provider_events(provider, provider_event_id, event_type, workspace_id, payload, status)
		VALUES($1,$2,$3,NULLIF($4,''),$5,'received')
		RETURNING id, provider, provider_event_id, event_type, workspace_id, status, failure_reason, received_at, processed_at`,
		provider, providerEventID, eventType, workspaceID, payload).Scan(
		&out.ID, &out.Provider, &out.ProviderEventID, &out.EventType, &out.WorkspaceID, &out.Status, &out.FailureReason, &out.ReceivedAt, &out.ProcessedAt)
	return out, mapDB(err)
}

func (r *Repository) GetProviderEvent(ctx context.Context, db DBTX, provider, providerEventID string) (ProviderEvent, error) {
	var out ProviderEvent
	err := db.QueryRow(ctx, `SELECT id, provider, provider_event_id, event_type, workspace_id, status, failure_reason, received_at, processed_at
		FROM billing_provider_events WHERE provider=$1 AND provider_event_id=$2`, provider, providerEventID).Scan(
		&out.ID, &out.Provider, &out.ProviderEventID, &out.EventType, &out.WorkspaceID, &out.Status, &out.FailureReason, &out.ReceivedAt, &out.ProcessedAt)
	return out, mapDB(err)
}

func (r *Repository) MarkProviderEvent(ctx context.Context, db DBTX, id, status string, failureReason *string, workspaceID *string) error {
	_, err := db.Exec(ctx, `UPDATE billing_provider_events
		SET status=$2, failure_reason=$3, workspace_id=COALESCE($4, workspace_id), processed_at=now()
		WHERE id=$1`, id, status, failureReason, workspaceID)
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
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return identity.ErrConflict
		case "23503":
			return identity.ErrNotFound
		case "23514":
			return identity.ErrValidation
		}
	}
	return err
}
