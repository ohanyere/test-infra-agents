package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// Config carries provider wiring. Secrets come from environment only.
type Config struct {
	Provider      string
	WebhookSecret string
}

// Service owns billing accounts, entitlements, the usage ledger, and
// provider reconciliation. Plan/status transitions are server-owned; clients
// can never submit plan, status, or balance values.
type Service struct {
	pool     *pgxpool.Pool
	repo     *Repository
	roles    roleRepository
	provider BillingProvider
	cfg      Config
}

// PrepareExecution performs the commercial checks that must happen before an
// execution transaction creates or queues runs. It also materializes the
// current period's idempotent allowance grant. Suite/plan creation does not
// call this method and therefore remains free.
func (s *Service) PrepareExecution(ctx context.Context, principal auth.Principal, workspaceID string) error {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return err
	}
	summary, account, err := s.usageNoAuth(ctx, workspaceID, principal.UserID)
	if err != nil {
		return err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return fmt.Errorf("unknown plan %q", account.PlanID)
	}
	if decision := CheckEntitlement(plan, account.Status, EntCanRunEvaluations, CheckOptions{}); !decision.Allowed {
		return entitlementDenied(decision.Code, decision.Reason)
	}
	if !summary.Unlimited && summary.Available < 1 {
		return entitlementDenied(CodeInsufficientCredits, "insufficient evaluation credits")
	}
	return nil
}

// ReserveSuiteExecution appends one reservation per run inside the same
// transaction that creates and queues the suite execution. The billing
// account row is locked, making the aggregate availability check and all
// inserts atomic across concurrent suite starts.
func (s *Service) ReserveSuiteExecution(ctx context.Context, tx pgx.Tx, workspaceID, executionID, userID string) error {
	account, err := s.repo.LockAccount(ctx, tx, workspaceID)
	if err != nil {
		return err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return fmt.Errorf("unknown plan %q", account.PlanID)
	}
	rows, err := tx.Query(ctx, `SELECT r.id, r.batch_id, b.scenario_version_id
		FROM suite_execution_batches seb
		JOIN evaluation_batches b ON b.id=seb.batch_id
		JOIN runs r ON r.batch_id=b.id
		WHERE seb.suite_execution_id=$1 ORDER BY r.id`, executionID)
	if err != nil {
		return err
	}
	type runPin struct{ runID, batchID, scenarioVersionID string }
	var runs []runPin
	for rows.Next() {
		var pin runPin
		if err := rows.Scan(&pin.runID, &pin.batchID, &pin.scenarioVersionID); err != nil {
			rows.Close()
			return err
		}
		runs = append(runs, pin)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	sums, err := s.repo.SumByKind(ctx, tx, workspaceID, account.PeriodStart)
	if err != nil {
		return err
	}
	if plan.MonthlyCredits >= 0 && Summarize(workspaceID, plan, account.PeriodStart, account.PeriodEnd, sums).Available < int64(len(runs)) {
		return entitlementDenied(CodeInsufficientCredits,
			fmt.Sprintf("insufficient evaluation credits: required %d", len(runs)))
	}
	for _, pin := range runs {
		runID, batchID, scenarioVersionID := pin.runID, pin.batchID, pin.scenarioVersionID
		_, err = s.repo.InsertEntry(ctx, tx, UsageEntry{
			WorkspaceID: workspaceID, Kind: UsageReservation, Quantity: MeterUnits(""),
			MeterContract: UsageMeterContractVersion, CatalogContract: BillingCatalogContractVersion,
			IdempotencyKey: "reservation:" + runID, SuiteExecutionID: &executionID,
			RunID: &runID, BatchID: &batchID, ScenarioVersionID: &scenarioVersionID,
			Reason: "evaluation execution reservation", CreatedBy: &userID,
		})
		if err != nil && !errors.Is(err, identity.ErrConflict) {
			return err
		}
	}
	return nil
}

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, provider BillingProvider, cfg Config) *Service {
	if provider == nil {
		provider = FakeProvider{}
	}
	if cfg.Provider == "" {
		cfg.Provider = "fake"
	}
	return &Service{pool: pool, repo: repo, roles: roles, provider: provider, cfg: cfg}
}

func (s *Service) ProviderName() string { return s.cfg.Provider }

// CurrentPeriod returns the calendar-month UTC billing period containing now.
func CurrentPeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func periodLabel(start time.Time) string { return start.UTC().Format("2006-01") }

// EnsureAccount returns the workspace billing account, creating a Developer
// free account deterministically when none exists. Historical workspaces are
// never placed on a paid plan by backfill.
func (s *Service) EnsureAccount(ctx context.Context, workspaceID string) (BillingAccount, error) {
	if err := identity.ValidateUUID(workspaceID); err != nil {
		return BillingAccount{}, fmt.Errorf("%w: workspace_id must be a valid UUID", identity.ErrValidation)
	}
	account, err := s.repo.GetAccount(ctx, s.pool, workspaceID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, identity.ErrNotFound) {
		return BillingAccount{}, err
	}
	start, end := CurrentPeriod(time.Now())
	account, err = s.repo.CreateAccount(ctx, s.pool, workspaceID, PlanDeveloper, StatusFree, start, end)
	if err != nil {
		if errors.Is(err, identity.ErrConflict) {
			return s.repo.GetAccount(ctx, s.pool, workspaceID)
		}
		return BillingAccount{}, err
	}
	return account, nil
}

// GetAccount reads the billing account. Any workspace member (including
// viewer) may read; historical reads remain available on canceled plans.
func (s *Service) GetAccount(ctx context.Context, principal auth.Principal, workspaceID string) (BillingAccount, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return BillingAccount{}, err
	}
	return s.EnsureAccount(ctx, workspaceID)
}

// Entitlements returns the full V1 entitlement snapshot.
func (s *Service) Entitlements(ctx context.Context, principal auth.Principal, workspaceID string, runtimeExists bool, requiredIsolation string) ([]EntitlementDecision, BillingAccount, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, BillingAccount{}, err
	}
	account, err := s.EnsureAccount(ctx, workspaceID)
	if err != nil {
		return nil, BillingAccount{}, err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return nil, BillingAccount{}, fmt.Errorf("unknown plan %q", account.PlanID)
	}
	return AllEntitlements(plan, account.Status, runtimeExists, requiredIsolation), account, nil
}

// Check evaluates one entitlement key. Both RBAC (checked by callers before
// this for write paths) and billing must pass.
func (s *Service) Check(ctx context.Context, principal auth.Principal, workspaceID, key string, opts CheckOptions) (EntitlementDecision, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return EntitlementDecision{}, err
	}
	account, err := s.EnsureAccount(ctx, workspaceID)
	if err != nil {
		return EntitlementDecision{}, err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return EntitlementDecision{}, fmt.Errorf("unknown plan %q", account.PlanID)
	}
	return CheckEntitlement(plan, account.Status, key, opts), nil
}

// Usage returns the current allowance/balance plus recent safe entries.
func (s *Service) Usage(ctx context.Context, principal auth.Principal, workspaceID string) (UsageSummary, BillingAccount, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return UsageSummary{}, BillingAccount{}, err
	}
	return s.usageNoAuth(ctx, workspaceID, principal.UserID)
}

func (s *Service) usageNoAuth(ctx context.Context, workspaceID, userID string) (UsageSummary, BillingAccount, error) {
	account, err := s.EnsureAccount(ctx, workspaceID)
	if err != nil {
		return UsageSummary{}, BillingAccount{}, err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return UsageSummary{}, BillingAccount{}, fmt.Errorf("unknown plan %q", account.PlanID)
	}
	now := time.Now().UTC()
	periodStart, periodEnd := CurrentPeriod(now)
	if account.PeriodStart.UTC().Truncate(time.Hour*24) != periodStart || account.PeriodEnd.UTC() != periodEnd {
		// Lazy period rollover: update the account period first.
		if err := s.repo.UpdatePeriod(ctx, s.pool, workspaceID, periodStart, periodEnd); err != nil {
			return UsageSummary{}, BillingAccount{}, err
		}
		account.PeriodStart, account.PeriodEnd = periodStart, periodEnd
	}
	if plan.MonthlyCredits >= 0 {
		// Idempotent monthly grant; conflicts mean already granted.
		_, _ = s.repo.InsertEntry(ctx, s.pool, UsageEntry{
			WorkspaceID: workspaceID, Kind: UsageGrant, Quantity: plan.MonthlyCredits,
			MeterContract: UsageMeterContractVersion, CatalogContract: BillingCatalogContractVersion,
			IdempotencyKey: "grant:" + periodLabel(periodStart),
			Reason:         "monthly plan allowance for " + plan.ID,
			CreatedBy:      &userID,
		})
	}
	sums, err := s.repo.SumByKind(ctx, s.pool, workspaceID, periodStart)
	if err != nil {
		return UsageSummary{}, BillingAccount{}, err
	}
	summary := Summarize(workspaceID, plan, periodStart, periodEnd, sums)
	recent, err := s.repo.RecentEntries(ctx, s.pool, workspaceID, 25)
	if err != nil {
		return UsageSummary{}, BillingAccount{}, err
	}
	summary.Entries = recent
	return summary, account, nil
}

// Summarize derives allowance/balance from immutable sums. Pure for testing.
func Summarize(workspaceID string, plan Plan, periodStart, periodEnd time.Time, sums map[string]int64) UsageSummary {
	if plan.MonthlyCredits < 0 {
		return UsageSummary{
			WorkspaceID: workspaceID, PeriodStart: periodStart, PeriodEnd: periodEnd,
			Allowance: -1, AllowanceLabel: "custom", Granted: sums[UsageGrant] + sums[UsageAdjustment],
			Reserved: 0, Committed: 0, Available: -1, Unlimited: true,
			MeterContract: UsageMeterContractVersion, Entries: []UsageEntry{},
		}
	}
	granted := sums[UsageGrant] + sums[UsageAdjustment]
	reserved := sums[UsageReservation] - sums[UsageCommit] - sums[UsageRelease]
	if reserved < 0 {
		reserved = 0
	}
	committed := sums[UsageCommit] - sums[UsageRefund]
	if committed < 0 {
		committed = 0
	}
	available := granted - reserved - committed
	return UsageSummary{
		WorkspaceID: workspaceID, PeriodStart: periodStart, PeriodEnd: periodEnd,
		Allowance: plan.MonthlyCredits, AllowanceLabel: itoa(int(plan.MonthlyCredits)),
		Granted: granted, Reserved: reserved, Committed: committed, Available: available,
		MeterContract: UsageMeterContractVersion, Entries: []UsageEntry{},
	}
}

// Reserve holds allowance before expensive execution. RBAC (member+) is
// checked first, then entitlement, then quota — all inside one transaction
// with a row lock so two concurrent requests cannot spend the same allowance.
// Idempotent on (workspace_id, idempotency_key).
func (s *Service) Reserve(ctx context.Context, principal auth.Principal, in ReserveInput) (UsageEntry, error) {
	if err := identity.ValidateUUID(in.WorkspaceID); err != nil {
		return UsageEntry{}, fmt.Errorf("%w: workspace_id must be a valid UUID", identity.ErrValidation)
	}
	if err := s.requireWrite(ctx, principal, in.WorkspaceID); err != nil {
		return UsageEntry{}, err
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return UsageEntry{}, fmt.Errorf("%w: idempotency_key is required", identity.ErrValidation)
	}
	if in.Quantity <= 0 {
		return UsageEntry{}, fmt.Errorf("%w: quantity must be positive", identity.ErrValidation)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UsageEntry{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, err := s.repo.GetEntryByIdempotency(ctx, tx, in.WorkspaceID, in.IdempotencyKey); err == nil {
		_ = tx.Commit(ctx)
		return existing, nil
	} else if !errors.Is(err, identity.ErrNotFound) {
		return UsageEntry{}, err
	}
	account, err := s.repo.LockAccount(ctx, tx, in.WorkspaceID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			_ = tx.Rollback(ctx)
			if _, ensureErr := s.EnsureAccount(ctx, in.WorkspaceID); ensureErr != nil {
				return UsageEntry{}, ensureErr
			}
			return s.Reserve(ctx, principal, in)
		}
		return UsageEntry{}, err
	}
	plan, ok := GetPlan(account.PlanID)
	if !ok {
		return UsageEntry{}, fmt.Errorf("unknown plan %q", account.PlanID)
	}
	// Entitlement gate: commercial state can block new execution.
	if decision := CheckEntitlement(plan, account.Status, EntCanRunEvaluations, CheckOptions{}); !decision.Allowed {
		code := decision.Code
		if code == "" {
			code = CodeEntitlementDenied
		}
		return UsageEntry{}, entitlementDenied(code, decision.Reason)
	}
	// Environment fidelity gate when isolation is pinned.
	if in.IsolationTier != nil && *in.IsolationTier != "" {
		if decision := CheckEntitlement(plan, account.Status, EntAllowedEnvironmentIsolation, CheckOptions{RequiredIsolation: *in.IsolationTier}); !decision.Allowed {
			return UsageEntry{}, entitlementDenied(decision.Code, decision.Reason)
		}
	}
	if plan.MonthlyCredits >= 0 {
		sums, err := s.repo.SumByKind(ctx, tx, in.WorkspaceID, account.PeriodStart)
		if err != nil {
			return UsageEntry{}, err
		}
		summary := Summarize(in.WorkspaceID, plan, account.PeriodStart, account.PeriodEnd, sums)
		if summary.Available < in.Quantity {
			return UsageEntry{}, entitlementDenied(CodeInsufficientCredits,
				fmt.Sprintf("insufficient evaluation credits: available %d, required %d", summary.Available, in.Quantity))
		}
	}
	entry, err := s.repo.InsertEntry(ctx, tx, UsageEntry{
		WorkspaceID: in.WorkspaceID, Kind: UsageReservation, Quantity: in.Quantity,
		MeterContract: UsageMeterContractVersion, CatalogContract: BillingCatalogContractVersion,
		IdempotencyKey:   in.IdempotencyKey,
		SuiteExecutionID: in.SuiteExecutionID, RunID: in.RunID, BatchID: in.BatchID,
		ScenarioVersionID: in.ScenarioVersionID, EnvironmentProfile: in.EnvironmentProfile,
		Provider: in.Provider, IsolationTier: in.IsolationTier,
		Reason: in.Reason, CreatedBy: in.CreatedBy,
	})
	if err != nil {
		if errors.Is(err, identity.ErrConflict) {
			existing, getErr := s.repo.GetEntryByIdempotency(ctx, tx, in.WorkspaceID, in.IdempotencyKey)
			if getErr != nil {
				return UsageEntry{}, getErr
			}
			_ = tx.Commit(ctx)
			return existing, nil
		}
		return UsageEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UsageEntry{}, err
	}
	return entry, nil
}

// Commit converts a reservation into billable usage. Idempotent on
// commit:<runID>. Double commit is impossible: repeats return the original.
func (s *Service) Commit(ctx context.Context, workspaceID, runID string, quantity int64, reason string) (UsageEntry, error) {
	return s.settleEntry(ctx, workspaceID, runID, UsageCommit, "commit:"+runID, quantity, reason, true)
}

// Release cancels a reservation without charge. Idempotent.
func (s *Service) Release(ctx context.Context, workspaceID, runID string, quantity int64, reason string) (UsageEntry, error) {
	return s.settleEntry(ctx, workspaceID, runID, UsageRelease, "release:"+runID, quantity, reason, true)
}

// Refund reverses a commit through an append-only correcting entry. History
// is never deleted. Idempotent.
func (s *Service) Refund(ctx context.Context, workspaceID, runID string, quantity int64, reason string) (UsageEntry, error) {
	return s.settleEntry(ctx, workspaceID, runID, UsageRefund, "refund:"+runID, quantity, reason, false)
}

func (s *Service) settleEntry(ctx context.Context, workspaceID, runID, kind, idempotencyKey string, quantity int64, reason string, requireReservation bool) (UsageEntry, error) {
	if err := identity.ValidateUUID(workspaceID); err != nil {
		return UsageEntry{}, fmt.Errorf("%w: workspace_id must be a valid UUID", identity.ErrValidation)
	}
	if err := identity.ValidateUUID(runID); err != nil {
		return UsageEntry{}, fmt.Errorf("%w: run_id must be a valid UUID", identity.ErrValidation)
	}
	if quantity <= 0 {
		quantity = 1
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UsageEntry{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, err := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, idempotencyKey); err == nil {
		_ = tx.Commit(ctx)
		return existing, nil
	} else if !errors.Is(err, identity.ErrNotFound) {
		return UsageEntry{}, err
	}
	if requireReservation {
		if _, err := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, "reservation:"+runID); err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				return UsageEntry{}, fmt.Errorf("%w: no reservation for run", identity.ErrValidation)
			}
			return UsageEntry{}, err
		}
	}
	switch kind {
	case UsageCommit:
		if _, err := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, "release:"+runID); err == nil {
			return UsageEntry{}, fmt.Errorf("%w: reservation already released", identity.ErrConflict)
		} else if !errors.Is(err, identity.ErrNotFound) {
			return UsageEntry{}, err
		}
	case UsageRelease:
		if _, err := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, "commit:"+runID); err == nil {
			return UsageEntry{}, fmt.Errorf("%w: reservation already committed; use refund", identity.ErrConflict)
		} else if !errors.Is(err, identity.ErrNotFound) {
			return UsageEntry{}, err
		}
	case UsageRefund:
		if _, err := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, "commit:"+runID); err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				return UsageEntry{}, fmt.Errorf("%w: no commit to refund", identity.ErrValidation)
			}
			return UsageEntry{}, err
		}
	}
	entry, err := s.repo.InsertEntry(ctx, tx, UsageEntry{
		WorkspaceID: workspaceID, Kind: kind, Quantity: quantity,
		MeterContract: UsageMeterContractVersion, CatalogContract: BillingCatalogContractVersion,
		IdempotencyKey: idempotencyKey, RunID: &runID, Reason: reason,
	})
	if err != nil {
		if errors.Is(err, identity.ErrConflict) {
			existing, getErr := s.repo.GetEntryByIdempotency(ctx, tx, workspaceID, idempotencyKey)
			if getErr != nil {
				return UsageEntry{}, getErr
			}
			_ = tx.Commit(ctx)
			return existing, nil
		}
		return UsageEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UsageEntry{}, err
	}
	return entry, nil
}

// SettleRun applies deterministic settlement for one terminal trial. It is
// idempotent: repeated worker completion callbacks never double-charge, and
// abandoned-worker recovery never charges twice.
func (s *Service) SettleRun(ctx context.Context, in SettleInput) (SettleAction, UsageEntry, error) {
	action := ClassifySettlement(in)
	quantity := MeterUnits(stringValue(in.IsolationTier))
	switch action.Action {
	case SettleCommit:
		entry, err := s.Commit(ctx, in.WorkspaceID, in.RunID, quantity, action.Reason)
		return action, entry, err
	case SettleRelease:
		entry, err := s.Release(ctx, in.WorkspaceID, in.RunID, quantity, action.Reason)
		// A missing reservation (e.g. pre-billing trial or already-settled
		// legacy row) settles to none rather than failing recovery.
		if err != nil && errors.Is(err, identity.ErrValidation) {
			return SettleAction{Action: SettleNone, Reason: "no reservation to release"}, UsageEntry{}, nil
		}
		return action, entry, err
	case SettleRefund:
		// Refund when a commit exists; otherwise release the reservation.
		if _, err := s.repo.GetEntryByIdempotency(ctx, s.pool, in.WorkspaceID, "commit:"+in.RunID); err == nil {
			entry, err := s.Refund(ctx, in.WorkspaceID, in.RunID, quantity, action.Reason)
			return action, entry, err
		}
		entry, err := s.Release(ctx, in.WorkspaceID, in.RunID, quantity, action.Reason)
		if err != nil && errors.Is(err, identity.ErrValidation) {
			return SettleAction{Action: SettleNone, Reason: "no reservation to release"}, UsageEntry{}, nil
		}
		action.Action = SettleRelease
		return action, entry, err
	default:
		return action, UsageEntry{}, nil
	}
}

// SettlePersistedRun reads the authoritative terminal/evaluation evidence and
// applies the corresponding append-only settlement. Workers call it only
// after terminal persistence and evaluation reconciliation.
func (s *Service) SettlePersistedRun(ctx context.Context, workspaceID, runID string) error {
	var in SettleInput
	in.WorkspaceID, in.RunID = workspaceID, runID
	var platformInvalid bool
	err := s.pool.QueryRow(ctx, `SELECT r.status, COALESCE(e.result,''),
		EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed'),
		COALESCE((SELECT re.payload->>'classification' FROM run_events re WHERE re.run_id=r.id AND re.payload ? 'classification' ORDER BY re.sequence DESC LIMIT 1),''),
		COALESCE((SELECT re.payload->>'code' FROM run_events re WHERE re.run_id=r.id AND re.payload ? 'code' ORDER BY re.sequence DESC LIMIT 1),'')
		FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id LEFT JOIN evaluations e ON e.run_id=r.id
		WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(
		&in.RunStatus, &in.EvaluationStatus, &platformInvalid, &in.FailureClass, &in.FailureCode)
	if err != nil {
		return mapDB(err)
	}
	if platformInvalid {
		in.Validity = "invalid_platform"
	} else if in.EvaluationStatus != "" {
		in.Validity = "valid"
	} else {
		in.Validity = "pending"
	}
	_, _, err = s.SettleRun(ctx, in)
	return err
}

// CheckParallelLimit enforces max_parallel_trials against an observed active
// count. Pure logic; callers supply the count from execution state.
func CheckParallelLimit(plan Plan, activeCount int) *BillingError {
	if activeCount >= plan.MaxParallelTrials {
		return entitlementDenied(CodePlanLimitReached,
			fmt.Sprintf("plan allows up to %d parallel trials", plan.MaxParallelTrials))
	}
	return nil
}

// CheckMemberLimit enforces max_workspace_members against an observed count.
func CheckMemberLimit(plan Plan, memberCount int) *BillingError {
	if plan.MaxMembers < 0 {
		return nil
	}
	if memberCount >= plan.MaxMembers {
		return entitlementDenied(CodePlanLimitReached,
			fmt.Sprintf("plan allows up to %d workspace members", plan.MaxMembers))
	}
	return nil
}

// CreateCheckoutSession creates a provider checkout flow. Owner/admin only;
// viewers and members cannot mutate billing.
func (s *Service) CreateCheckoutSession(ctx context.Context, principal auth.Principal, workspaceID, planID string) (string, error) {
	if err := s.requireAdmin(ctx, principal, workspaceID); err != nil {
		return "", err
	}
	if !IsValidPlan(planID) {
		return "", fmt.Errorf("%w: unknown plan", identity.ErrValidation)
	}
	if _, err := s.EnsureAccount(ctx, workspaceID); err != nil {
		return "", err
	}
	url, _, err := s.provider.CreateCheckoutSession(ctx, workspaceID, planID)
	return url, err
}

// CreatePortalSession creates a provider customer-portal flow. Owner/admin only.
func (s *Service) CreatePortalSession(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	if err := s.requireAdmin(ctx, principal, workspaceID); err != nil {
		return "", err
	}
	if _, err := s.EnsureAccount(ctx, workspaceID); err != nil {
		return "", err
	}
	return s.provider.CreatePortalSession(ctx, workspaceID)
}

// HandleProviderWebhook persists an idempotent event inbox row, verifies the
// signature before any state mutation, and reconciles subscription state.
// Duplicate events are no-ops; stale events never overwrite newer truth.
func (s *Service) HandleProviderWebhook(ctx context.Context, provider string, rawBody []byte, signature string) (ProviderEvent, error) {
	if provider == "" {
		provider = s.cfg.Provider
	}
	if provider != s.provider.Name() {
		return ProviderEvent{}, fmt.Errorf("%w: unknown billing provider", identity.ErrValidation)
	}
	if !VerifySignature(s.cfg.WebhookSecret, rawBody, signature) {
		return ProviderEvent{}, fmt.Errorf("%w: invalid webhook signature", identity.ErrValidation)
	}
	eventID, eventType, err := s.provider.VerifyWebhook(rawBody, signature)
	if err != nil {
		return ProviderEvent{}, err
	}
	reconciliation, err := s.provider.ParseEvent(rawBody)
	if err != nil {
		return ProviderEvent{}, err
	}
	safePayload, _ := jsonMarshalSafe(reconciliation)
	existing, err := s.repo.GetProviderEvent(ctx, s.pool, provider, eventID)
	if err == nil {
		if existing.Status == ProviderEventApplied || existing.Status == ProviderEventDuplicate {
			dup := existing
			dup.Status = ProviderEventDuplicate
			return dup, nil
		}
		// Reprocess a previously failed event exactly once per delivery.
	} else if !errors.Is(err, identity.ErrNotFound) {
		return ProviderEvent{}, err
	}
	var record ProviderEvent
	if errors.Is(err, identity.ErrNotFound) {
		record, err = s.repo.InsertProviderEvent(ctx, s.pool, provider, eventID, eventType, reconciliation.WorkspaceID, safePayload)
		if err != nil {
			if errors.Is(err, identity.ErrConflict) {
				existing, getErr := s.repo.GetProviderEvent(ctx, s.pool, provider, eventID)
				if getErr != nil {
					return ProviderEvent{}, getErr
				}
				return existing, nil
			}
			return ProviderEvent{}, err
		}
	} else {
		record = existing
	}
	if reconciliation.WorkspaceID == "" {
		reason := "event carries no workspace scope"
		_ = s.repo.MarkProviderEvent(ctx, s.pool, record.ID, ProviderEventFailed, &reason, nil)
		record.Status = ProviderEventFailed
		return record, nil
	}
	account, err := s.EnsureAccount(ctx, reconciliation.WorkspaceID)
	if err != nil {
		reason := err.Error()
		_ = s.repo.MarkProviderEvent(ctx, s.pool, record.ID, ProviderEventFailed, &reason, &reconciliation.WorkspaceID)
		return record, err
	}
	// Stale guard: provider events carry UpdatedAt; never overwrite newer
	// subscription truth with older events.
	if !reconciliation.UpdatedAt.IsZero() && reconciliation.UpdatedAt.Before(account.UpdatedAt.Add(-time.Second)) {
		reason := "stale provider event predates current subscription truth"
		_ = s.repo.MarkProviderEvent(ctx, s.pool, record.ID, ProviderEventStale, &reason, &reconciliation.WorkspaceID)
		record.Status = ProviderEventStale
		return record, nil
	}
	planID := account.PlanID
	if reconciliation.PlanID != "" {
		planID = reconciliation.PlanID
	}
	status := account.Status
	if reconciliation.Status != "" {
		status = reconciliation.Status
	}
	periodStart, periodEnd := account.PeriodStart, account.PeriodEnd
	if status == StatusActive || status == StatusTrialing {
		periodStart, periodEnd = CurrentPeriod(time.Now())
	}
	if err := s.repo.UpdateSubscription(ctx, s.pool, reconciliation.WorkspaceID, planID, status,
		provider, reconciliation.CustomerRef, reconciliation.SubscriptionRef, periodStart, periodEnd); err != nil {
		reason := err.Error()
		_ = s.repo.MarkProviderEvent(ctx, s.pool, record.ID, ProviderEventFailed, &reason, &reconciliation.WorkspaceID)
		return record, err
	}
	_ = s.repo.MarkProviderEvent(ctx, s.pool, record.ID, ProviderEventApplied, nil, &reconciliation.WorkspaceID)
	record.Status = ProviderEventApplied
	return record, nil
}

func (s *Service) role(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func (s *Service) requireWrite(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, principal, workspaceID)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	default:
		return auth.ErrForbidden
	}
}

func (s *Service) requireAdmin(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, principal, workspaceID)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin":
		return nil
	default:
		return auth.ErrForbidden
	}
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func jsonMarshalSafe(v any) (string, error) {
	// Minimal safe encoding without importing encoding/json at call sites.
	// Keep payload small: reconciliation fields only, never secrets.
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(raw) > 4096 {
		raw = raw[:4096]
	}
	return string(raw), nil
}
