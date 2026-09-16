package trajectory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository loads existing persisted evidence. It creates no new tables:
// the explorer is a projection over runs, run_events, agent_actions,
// evaluations (+ criteria + findings), suite executions, and environment
// instances.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

type LoadedTrial struct {
	Run        RunInfo
	Events     []evidence.RunEvent
	Actions    []evidence.AgentAction
	Evaluation EvaluationInfo
	Meta       TrialMeta
}

func (r *Repository) LoadTrial(ctx context.Context, db *pgxpool.Pool, workspaceID, runID string) (LoadedTrial, error) {
	var out LoadedTrial
	var envRef *string
	var startedAt, completedAt *time.Time
	err := db.QueryRow(ctx, `SELECT r.id,r.batch_id,b.workspace_id,r.agent_version_id,b.scenario_version_id,r.attempt_number,r.status,r.environment_ref,r.created_at,r.started_at,r.completed_at
		FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).
		Scan(&out.Run.ID, &out.Run.BatchID, &out.Run.WorkspaceID, &out.Run.AgentVersionID, &out.Run.ScenarioVersionID, &out.Run.AttemptNumber, &out.Run.Status, &envRef, &out.Run.CreatedAt, &startedAt, &completedAt)
	if err != nil {
		return out, mapDB(err)
	}
	out.Run.EnvironmentRef = envRef
	out.Run.StartedAt = startedAt
	out.Run.CompletedAt = completedAt
	// Cross-workspace ownership is enforced by the workspace predicate above;
	// any row returned provably belongs to this workspace/run pair.
	if out.Run.WorkspaceID != workspaceID || out.Run.ID != runID {
		return out, identity.ErrNotFound
	}

	rows, err := db.Query(ctx, `SELECT e.id,e.run_id,e.sequence,e.event_type,e.occurred_at,e.summary,e.payload,e.created_at FROM run_events e WHERE e.run_id=$1 ORDER BY e.sequence ASC,e.id ASC`, runID)
	if err != nil {
		return out, mapDB(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item evidence.RunEvent
		if err = rows.Scan(&item.ID, &item.RunID, &item.Sequence, &item.EventType, &item.OccurredAt, &item.Summary, &item.Payload, &item.CreatedAt); err != nil {
			return out, mapDB(err)
		}
		if item.RunID != runID {
			return out, fmt.Errorf("%w: action belongs to another run", identity.ErrConflict)
		}
		out.Events = append(out.Events, item)
	}
	if err = rows.Err(); err != nil {
		return out, mapDB(err)
	}

	actionRows, err := db.Query(ctx, `SELECT a.id,a.run_id,a.run_event_id,a.action_type,a.target_type,a.target_ref,a.status,a.authorized,a.request_payload,a.result_payload,a.started_at,a.completed_at,a.created_at FROM agent_actions a WHERE a.run_id=$1 ORDER BY a.created_at ASC,a.id ASC`, runID)
	if err != nil {
		return out, mapDB(err)
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var item evidence.AgentAction
		if err = actionRows.Scan(&item.ID, &item.RunID, &item.RunEventID, &item.ActionType, &item.TargetType, &item.TargetRef, &item.Status, &item.Authorized, &item.RequestPayload, &item.ResultPayload, &item.StartedAt, &item.CompletedAt, &item.CreatedAt); err != nil {
			return out, mapDB(err)
		}
		if item.RunID != runID {
			return out, fmt.Errorf("%w: action belongs to another run", identity.ErrConflict)
		}
		out.Actions = append(out.Actions, item)
	}
	if err = actionRows.Err(); err != nil {
		return out, mapDB(err)
	}

	var evalID, result, evaluatorVersion, summary string
	var recoveryMS *int64
	var availability, peakError *float64
	var safetyCount int
	var evalCompletedAt *time.Time
	evalErr := db.QueryRow(ctx, `SELECT e.id,e.result,e.evaluator_version,e.summary,e.recovery_duration_ms,e.availability_pct,e.peak_error_rate_pct,e.safety_violation_count,e.completed_at FROM evaluations e JOIN runs r ON r.id=e.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE e.run_id=$1 AND b.workspace_id=$2`, runID, workspaceID).
		Scan(&evalID, &result, &evaluatorVersion, &summary, &recoveryMS, &availability, &peakError, &safetyCount, &evalCompletedAt)
	if evalErr == nil {
		out.Evaluation = EvaluationInfo{Found: true, ID: evalID, Result: result, EvaluatorVersion: evaluatorVersion, RecoveryDurationMS: recoveryMS, AvailabilityPct: availability, PeakErrorRatePct: peakError, SafetyViolationCount: safetyCount, CompletedAt: evalCompletedAt}
		criteria, err := loadCriteria(ctx, db, evalID)
		if err != nil {
			return out, err
		}
		out.Evaluation.Criteria = criteria
		findings, err := loadFindings(ctx, db, evalID)
		if err != nil {
			return out, err
		}
		out.Evaluation.Findings = findings
	} else if !errors.Is(evalErr, pgx.ErrNoRows) {
		return out, mapDB(evalErr)
	}

	metaErr := db.QueryRow(ctx, `SELECT rv.suite_execution_id,rv.suite_version_id,rv.variant_id,rv.variant_seed,rv.trial_ordinal FROM suite_execution_run_variants rv JOIN suite_executions se ON se.id=rv.suite_execution_id WHERE rv.run_id=$1 AND se.workspace_id=$2`, runID, workspaceID).
		Scan(&out.Meta.SuiteExecutionID, &out.Meta.SuiteVersionID, &out.Meta.VariantID, &out.Meta.VariantSeed, &out.Meta.TrialOrdinal)
	if metaErr != nil && !errors.Is(metaErr, pgx.ErrNoRows) {
		return out, mapDB(metaErr)
	}
	// Hidden variant contents are never selected: only the opaque variant ID.
	var profileID, providerID, isolation *string
	envErr := db.QueryRow(ctx, `SELECT ei.profile_id,ei.provider_id,ei.isolation_tier FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=$1 ORDER BY ei.created_at DESC LIMIT 1`, runID).
		Scan(&profileID, &providerID, &isolation)
	if envErr == nil {
		out.Meta.EnvironmentProfileID = profileID
		out.Meta.EnvironmentProviderID = providerID
		out.Meta.EnvironmentIsolationTier = isolation
	} else if !errors.Is(envErr, pgx.ErrNoRows) {
		return out, mapDB(envErr)
	}
	out.Meta.RuntimeContract = "runtime-contract:v1"
	if out.Meta.TrialOrdinal == 0 {
		out.Meta.TrialOrdinal = out.Run.AttemptNumber
	}
	return out, nil
}

func loadCriteria(ctx context.Context, db *pgxpool.Pool, evaluationID string) ([]evidence.CriterionResult, error) {
	rows, err := db.Query(ctx, `SELECT cr.id,cr.scenario_criterion_id,c.key,c.name,c.metric_key,c.operator,c.target_value,c.unit,cr.passed,cr.observed_value,cr.evidence,cr.created_at FROM evaluation_criterion_results cr JOIN scenario_criteria c ON c.id=cr.scenario_criterion_id WHERE cr.evaluation_id=$1 ORDER BY c.sort_order ASC,c.id ASC`, evaluationID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := []evidence.CriterionResult{}
	for rows.Next() {
		var item evidence.CriterionResult
		if err = rows.Scan(&item.ID, &item.ScenarioCriterionID, &item.Key, &item.Name, &item.MetricKey, &item.Operator, &item.TargetValue, &item.Unit, &item.Passed, &item.ObservedValue, &item.Evidence, &item.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, item)
	}
	return items, mapDB(rows.Err())
}

func loadFindings(ctx context.Context, db *pgxpool.Pool, evaluationID string) ([]evidence.Finding, error) {
	rows, err := db.Query(ctx, `SELECT id,kind,summary,evidence,sort_order,created_at FROM evaluation_findings WHERE evaluation_id=$1 ORDER BY sort_order ASC,created_at ASC,id ASC`, evaluationID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := []evidence.Finding{}
	for rows.Next() {
		var item evidence.Finding
		if err = rows.Scan(&item.ID, &item.Kind, &item.Summary, &item.Evidence, &item.SortOrder, &item.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, item)
	}
	return items, mapDB(rows.Err())
}

// SuiteRow is one trial row for suite summaries.
type SuiteRow struct {
	RunID             string
	TrialOrdinal      int
	AgentVersionID    string
	ScenarioVersionID string
	RunStatus         string
	EvaluationResult  string
	PlatformInvalid   bool
	SafetyViolations  int
	EvaluatorVersion  string
	VariantID         string
}

func (r *Repository) ListSuiteTrials(ctx context.Context, db *pgxpool.Pool, workspaceID, executionID string) ([]SuiteRow, error) {
	rows, err := db.Query(ctx, `SELECT r.id,COALESCE(rv.trial_ordinal,r.attempt_number),r.agent_version_id,seb.scenario_version_id,r.status,COALESCE(e.result,''),
		EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed') OR
		EXISTS(SELECT 1 FROM evaluation_findings pf JOIN evaluations pe ON pe.id=pf.evaluation_id AND pe.run_id=r.id WHERE pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable')),
		COALESCE(e.safety_violation_count,0),COALESCE(e.evaluator_version,''),COALESCE(rv.variant_id,'')
		FROM suite_executions se JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		WHERE se.id=$1 AND se.workspace_id=$2 ORDER BY seb.scenario_version_id,r.agent_version_id,r.attempt_number,r.id`, executionID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []SuiteRow{}
	for rows.Next() {
		var row SuiteRow
		if err = rows.Scan(&row.RunID, &row.TrialOrdinal, &row.AgentVersionID, &row.ScenarioVersionID, &row.RunStatus, &row.EvaluationResult, &row.PlatformInvalid, &row.SafetyViolations, &row.EvaluatorVersion, &row.VariantID); err != nil {
			return nil, mapDB(err)
		}
		out = append(out, row)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) EnsureSuiteWorkspace(ctx context.Context, db *pgxpool.Pool, workspaceID, executionID string) error {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM suite_executions WHERE id=$1 AND workspace_id=$2`, executionID, workspaceID).Scan(&id)
	return mapDB(err)
}

// RunExists reports whether a run row exists in any workspace. It is used
// only to distinguish a truly missing/deleted trial (partial summary allowed)
// from an ownership/integrity mismatch (fail the request, never silently
// omit). Callers must not expose cross-workspace identifiers.
func (r *Repository) RunExists(ctx context.Context, db *pgxpool.Pool, runID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE id=$1)`, runID).Scan(&exists)
	if err != nil {
		return false, mapDB(err)
	}
	return exists, nil
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
		}
	}
	return fmt.Errorf("trajectory database operation: %w", err)
}

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// Service owns the trajectory read model.
type Service struct {
	pool  *pgxpool.Pool
	repo  *Repository
	roles roleRepository
}

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles}
}

func (s *Service) requireRead(ctx context.Context, principal auth.Principal, workspaceID string) error {
	_, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return auth.ErrForbidden
	}
	return err
}

// GetTrajectory projects one trial with deterministic pagination + filters.
// Pagination is stable (ordering is total); the response always reports
// total_events and truncated so clients never silently see a prefix.
func (s *Service) GetTrajectory(ctx context.Context, principal auth.Principal, workspaceID, runID string, filters Filters) (Trajectory, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return Trajectory{}, err
	}
	if err := ValidateFilters(filters); err != nil {
		return Trajectory{}, err
	}
	loaded, err := s.repo.LoadTrial(ctx, s.pool, workspaceID, runID)
	if err != nil {
		return Trajectory{}, err
	}
	full := Project(loaded.Run, loaded.Events, loaded.Actions, loaded.Evaluation, loaded.Meta)
	filtered := make([]TrajectoryEvent, 0, len(full.Events))
	for _, event := range full.Events {
		if filters.Phase != "" && event.Phase != filters.Phase {
			continue
		}
		if filters.EventType != "" && event.EventType != filters.EventType {
			continue
		}
		if filters.Actor != "" && event.Actor != filters.Actor {
			continue
		}
		if filters.Status != "" && event.Status != filters.Status {
			continue
		}
		filtered = append(filtered, event)
	}
	total := len(filtered)
	start := filters.Offset
	if start > total {
		start = total
	}
	end := start + filters.Limit
	if filters.Limit <= 0 || end > total {
		end = total
	}
	page := filtered[start:end]
	for index := range page {
		sequence := int64(start + index)
		page[index].Sequence = sequence
	}
	full.Events = page
	full.TotalEvents = total
	full.Truncated = end < total
	return full, nil
}

// GetEvidence returns one bounded safe evidence item by stable reference.
func (s *Service) GetEvidence(ctx context.Context, principal auth.Principal, workspaceID, runID, evidenceID string) (EvidenceDetail, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return EvidenceDetail{}, err
	}
	if strings.TrimSpace(evidenceID) == "" {
		return EvidenceDetail{}, fmt.Errorf("%w: evidence_id is required", identity.ErrValidation)
	}
	loaded, err := s.repo.LoadTrial(ctx, s.pool, workspaceID, runID)
	if err != nil {
		return EvidenceDetail{}, err
	}
	full := Project(loaded.Run, loaded.Events, loaded.Actions, loaded.Evaluation, loaded.Meta)
	for _, event := range full.Events {
		if event.EventID != evidenceID {
			continue
		}
		detail, _ := json.Marshal(map[string]any{
			"event_type": event.EventType, "phase": event.Phase, "actor": event.Actor,
			"status": event.Status, "view": event.View, "metadata": json.RawMessage(event.SafeMetadata),
		})
		preview, truncated, size, hash := BoundedPreview(string(detail))
		return EvidenceDetail{
			EvidenceID: evidenceID, SourceType: event.SourceType, SourceID: event.SourceID,
			OccurredAt: &event.OccurredAt, Summary: event.Summary, View: event.View,
			SafeDetail: RedactRawMessage(detail), Preview: preview, Truncated: truncated,
			ByteSize: size, ContentHash: hash, Provenance: event.EvidenceRefs,
		}, nil
	}
	// Direct lookups for evaluation/criterion/finding refs.
	for _, event := range full.Events {
		for _, ref := range event.EvidenceRefs {
			if ref != evidenceID {
				continue
			}
			detail, _ := json.Marshal(map[string]any{"linked_event": event.EventID, "view": event.View})
			preview, truncated, size, hash := BoundedPreview(string(detail))
			return EvidenceDetail{
				EvidenceID: evidenceID, SourceType: event.SourceType, SourceID: event.SourceID,
				OccurredAt: &event.OccurredAt, Summary: event.Summary, View: event.View,
				SafeDetail: RedactRawMessage(detail), Preview: preview, Truncated: truncated,
				ByteSize: size, ContentHash: hash, Provenance: []string{event.EventID},
			}, nil
		}
	}
	return EvidenceDetail{}, identity.ErrNotFound
}

// SuiteSummary projects concise per-trial summaries for a suite execution.
// Integrity: trials that fail to load are never silently omitted. The response
// always reports complete/expected/loaded counts plus missing trial references
// and safe issue codes so the client can determine completeness.
// Ownership/integrity mismatches fail the request instead of returning a
// misleading partial report; truly missing/deleted rows yield complete=false.
func (s *Service) SuiteSummary(ctx context.Context, principal auth.Principal, workspaceID, executionID string) (SuiteTrajectorySummary, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return SuiteTrajectorySummary{}, err
	}
	if err := s.repo.EnsureSuiteWorkspace(ctx, s.pool, workspaceID, executionID); err != nil {
		return SuiteTrajectorySummary{}, err
	}
	rows, err := s.repo.ListSuiteTrials(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return SuiteTrajectorySummary{}, err
	}
	return summarizeSuiteTrials(executionID, workspaceID, rows,
		func(runID string) (LoadedTrial, error) {
			return s.repo.LoadTrial(ctx, s.pool, workspaceID, runID)
		},
		func(runID string) (bool, error) {
			return s.repo.RunExists(ctx, s.pool, runID)
		})
}

// summarizeSuiteTrials assembles a suite summary with explicit integrity
// accounting. It is pure apart from the injected loader/existence closures so
// focused tests can prove omission, partial, and ownership behavior without a
// database.
func summarizeSuiteTrials(executionID, workspaceID string, rows []SuiteRow, load func(runID string) (LoadedTrial, error), exists func(runID string) (bool, error)) (SuiteTrajectorySummary, error) {
	out := SuiteTrajectorySummary{
		Contract: ContractVersion, SuiteExecutionID: executionID, WorkspaceID: workspaceID,
		Trials: []SuiteTrialRow{}, MissingTrials: []string{}, Issues: []SuiteIssue{},
		ExpectedTrials: len(rows),
	}
	for _, row := range rows {
		loaded, err := load(row.RunID)
		if err == nil {
			full := Project(loaded.Run, loaded.Events, loaded.Actions, loaded.Evaluation, loaded.Meta)
			out.Trials = append(out.Trials, SuiteTrialRow{
				RunID: row.RunID, TrialOrdinal: row.TrialOrdinal, AgentVersionID: row.AgentVersionID,
				ScenarioVersionID: row.ScenarioVersionID, Outcome: full.Summary.PassFailOutcome,
				Validity:         full.Summary.Validity,
				ActionsRequested: full.Summary.ActionsRequested,
				ActionsExecuted:  full.Summary.ActionsExecuted,
				ActionsDenied:    full.Summary.ActionsDenied,
				SafetyViolations: full.Summary.SafetyViolationCount,
				TimeToRecoveryMS: full.Summary.TimeToRecoveryMS,
				RecoveryVerified: full.Summary.RecoveryVerified,
			})
			continue
		}
		// Ownership/integrity violations fail fast: never silently skip, never
		// leak cross-workspace identifiers (safe conflict code only).
		if errors.Is(err, identity.ErrConflict) {
			return SuiteTrajectorySummary{}, err
		}
		if errors.Is(err, identity.ErrNotFound) {
			found, existsErr := exists(row.RunID)
			if existsErr != nil {
				return SuiteTrajectorySummary{}, existsErr
			}
			if found {
				// Row exists but is not visible in this workspace: ownership
				// invariant violated. Fail instead of returning partial.
				return SuiteTrajectorySummary{}, fmt.Errorf("%w: trial ownership mismatch", identity.ErrConflict)
			}
			// Truly missing/deleted/unavailable: partial allowed, but explicit.
			runID := row.RunID
			out.MissingTrials = append(out.MissingTrials, runID)
			out.Issues = append(out.Issues, SuiteIssue{Code: "trial_unavailable", RunID: &runID, Detail: "trial could not be loaded"})
			continue
		}
		return SuiteTrajectorySummary{}, err
	}
	out.LoadedTrials = len(out.Trials)
	out.TotalTrials = len(rows)
	out.Complete = len(out.MissingTrials) == 0
	if len(out.MissingTrials) == 0 {
		out.MissingTrials = nil
		out.Issues = nil
	}
	return out, nil
}

// Compare deterministically compares two compatible trials (summary-level only).
func (s *Service) Compare(ctx context.Context, principal auth.Principal, workspaceID, baselineRunID, candidateRunID string) (ComparisonSummary, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return ComparisonSummary{}, err
	}
	baseline, err := s.repo.LoadTrial(ctx, s.pool, workspaceID, baselineRunID)
	if err != nil {
		return ComparisonSummary{}, err
	}
	candidate, err := s.repo.LoadTrial(ctx, s.pool, workspaceID, candidateRunID)
	if err != nil {
		return ComparisonSummary{}, err
	}
	base := Project(baseline.Run, baseline.Events, baseline.Actions, baseline.Evaluation, baseline.Meta)
	other := Project(candidate.Run, candidate.Events, candidate.Actions, candidate.Evaluation, candidate.Meta)
	return ComparisonSummary{
		Contract: ContractVersion, BaselineRunID: baselineRunID, CandidateRunID: candidateRunID,
		BaselineOutcome: base.Summary.PassFailOutcome, CandidateOutcome: other.Summary.PassFailOutcome,
		BaselineActions: base.Summary.ActionsRequested, CandidateActions: other.Summary.ActionsRequested,
		BaselineDenied: base.Summary.ActionsDenied, CandidateDenied: other.Summary.ActionsDenied,
		BaselineRecoveryMS: base.Summary.TimeToRecoveryMS, CandidateRecoveryMS: other.Summary.TimeToRecoveryMS,
		BaselineVerified: base.Summary.RecoveryVerified, CandidateVerified: other.Summary.RecoveryVerified,
		BaselineFindings: base.Summary.FindingCount, CandidateFindings: other.Summary.FindingCount,
		BaselineFailureClasses: base.Summary.FailureClasses, CandidateFailureClasses: other.Summary.FailureClasses,
	}, nil
}
