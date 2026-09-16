package scenariocertification

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimal database surface used by this package.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository owns certification SQL. Historical rows are never mutated.
type Repository struct{}

// NewRepository creates a certification Repository.
func NewRepository() *Repository { return &Repository{} }

// LockScenarioVersion serializes concurrent certification attempts on one
// ScenarioVersion using the existing row-lock pattern. Call inside a tx.
func (r *Repository) LockScenarioVersion(ctx context.Context, tx pgx.Tx, scenarioID, versionID string) error {
	var dummy string
	err := tx.QueryRow(ctx, `SELECT sv.id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2 FOR UPDATE`, versionID, scenarioID).Scan(&dummy)
	return mapDB(err)
}

// UpsertReference stores evaluator-only reference remediation. Never returned
// through agent-facing reads; only the certification service loads it.
func (r *Repository) UpsertReference(ctx context.Context, db DBTX, workspaceID, versionID string, in ReferenceRemediationInput, hash, createdBy string) (ReferenceRemediation, error) {
	var out ReferenceRemediation
	actions, _ := json.Marshal(in.ReferenceActions)
	err := db.QueryRow(ctx, `INSERT INTO scenario_version_reference_remediations
		(scenario_version_id, workspace_id, preconditions, reference_actions, expected_resulting_state, verification_notes, cleanup_expectations, content_hash, created_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid, now())
		ON CONFLICT (scenario_version_id) DO UPDATE SET workspace_id=EXCLUDED.workspace_id,
			preconditions=EXCLUDED.preconditions, reference_actions=EXCLUDED.reference_actions,
			expected_resulting_state=EXCLUDED.expected_resulting_state, verification_notes=EXCLUDED.verification_notes,
			cleanup_expectations=EXCLUDED.cleanup_expectations, content_hash=EXCLUDED.content_hash, updated_at=now()
		RETURNING scenario_version_id, contract_version, preconditions, reference_actions, expected_resulting_state, verification_notes, cleanup_expectations, content_hash, created_at, updated_at`,
		versionID, workspaceID, in.Preconditions, actions, in.ExpectedState, in.VerificationNotes, in.CleanupExpected, hash, createdBy).
		Scan(&out.ScenarioVersionID, &out.ContractVersion, &out.Preconditions, &out.ReferenceActions, &out.ExpectedState, &out.VerificationNotes, &out.CleanupExpected, &out.ContentHash, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	return out, nil
}

// GetReference loads evaluator-only reference remediation for certification.
// Service-gated: never expose through HTTP agent surfaces.
func (r *Repository) GetReference(ctx context.Context, db DBTX, versionID string) (ReferenceRemediation, error) {
	var out ReferenceRemediation
	actions := []byte{}
	err := db.QueryRow(ctx, `SELECT scenario_version_id, contract_version, preconditions, reference_actions, expected_resulting_state, verification_notes, cleanup_expectations, content_hash, created_at, updated_at
		FROM scenario_version_reference_remediations WHERE scenario_version_id=$1`, versionID).
		Scan(&out.ScenarioVersionID, &out.ContractVersion, &out.Preconditions, &actions, &out.ExpectedState, &out.VerificationNotes, &out.CleanupExpected, &out.ContentHash, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	if err := json.Unmarshal(actions, &out.ReferenceActions); err != nil {
		return out, err
	}
	return out, nil
}

// InsertCertification persists one attempt. Idempotent retry uses the
// (scenario_version_id, idempotency_key) unique constraint with ON CONFLICT DO
// NOTHING then SELECT.
func (r *Repository) InsertCertification(ctx context.Context, db DBTX, c Certification) (Certification, error) {
	gates, _ := json.Marshal(c.GateResults)
	var out Certification
	err := db.QueryRow(ctx, `INSERT INTO scenario_certifications
		(workspace_id, scenario_id, scenario_version_id, state, certification_contract_version,
		 reference_remediation_contract_version, reference_content_hash, evaluator_version,
		 runtime_contract_version, environment_profile, provider_id, provider_contract_version,
		 isolation_tier, environment_profile_contract_version, environment_component_contract_version,
		 criteria_version, hidden_variant_contract_version,
		 task_family_id, case_type, gate_results, failure_codes, reference_evaluation_id,
		 variant_ids, variant_seeds, idempotency_key, started_at, completed_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,NULLIF($13,''),$14,$15,$16,$17,NULLIF($18,''),NULLIF($19,''),$20,$21,$22,$23,$24,NULLIF($25,''),$26,$27,NULLIF($28,'')::uuid)
		ON CONFLICT (scenario_version_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING id, workspace_id, scenario_id, scenario_version_id, state, certification_contract_version,
			reference_remediation_contract_version, reference_content_hash, evaluator_version,
			runtime_contract_version, environment_profile, provider_id, provider_contract_version,
			isolation_tier, environment_profile_contract_version, environment_component_contract_version,
			criteria_version, hidden_variant_contract_version,
			task_family_id, case_type, gate_results, failure_codes, reference_evaluation_id,
			variant_ids, variant_seeds, idempotency_key, started_at, completed_at`,
		c.WorkspaceID, c.ScenarioID, c.ScenarioVersionID, c.State, c.CertificationContractVersion,
		c.ReferenceRemediationContractVersion, c.ReferenceContentHash, c.EvaluatorVersion,
		c.RuntimeContractVersion, c.EnvironmentProfile, c.ProviderID, c.ProviderContractVersion,
		c.IsolationTier, c.EnvironmentProfileContractVersion, c.EnvironmentComponentContractVersion,
		c.CriteriaVersion, c.HiddenVariantContractVersion,
		c.TaskFamilyID, c.CaseType, gates, c.FailureCodes, c.ReferenceEvaluationID,
		c.VariantIDs, c.VariantSeeds, nullableStr(c.IdempotencyKey), c.StartedAt, c.CompletedAt,
		"" /* created_by intentionally unrecorded; see CreatedByHidden */).
		Scan(&out.ID, &out.WorkspaceID, &out.ScenarioID, &out.ScenarioVersionID, &out.State, &out.CertificationContractVersion,
			&out.ReferenceRemediationContractVersion, &out.ReferenceContentHash, &out.EvaluatorVersion,
			&out.RuntimeContractVersion, &out.EnvironmentProfile, &out.ProviderID, &out.ProviderContractVersion,
			&out.IsolationTier, &out.EnvironmentProfileContractVersion, &out.EnvironmentComponentContractVersion,
			&out.CriteriaVersion, &out.HiddenVariantContractVersion,
			&out.TaskFamilyID, &out.CaseType, &gates, &out.FailureCodes, &out.ReferenceEvaluationID,
			&out.VariantIDs, &out.VariantSeeds, &out.IdempotencyKey, &out.StartedAt, &out.CompletedAt)
	if err != nil {
		return out, mapDB(err)
	}
	if err := json.Unmarshal(gates, &out.GateResults); err != nil {
		return out, err
	}
	if out.GateResults == nil {
		out.GateResults = []GateResult{}
	}
	if out.FailureCodes == nil {
		out.FailureCodes = []string{}
	}
	if out.VariantIDs == nil {
		out.VariantIDs = []string{}
	}
	if out.VariantSeeds == nil {
		out.VariantSeeds = []int64{}
	}
	return out, nil
}

// GetByIdempotency returns an existing attempt for safe retry.
func (r *Repository) GetByIdempotency(ctx context.Context, db DBTX, versionID, key string) (Certification, error) {
	return r.scanCertification(db.QueryRow(ctx, certificationSelect+` WHERE scenario_version_id=$1 AND idempotency_key=$2 ORDER BY created_at DESC, id DESC LIMIT 1`, versionID, key))
}

// Latest returns the newest attempt for one ScenarioVersion.
func (r *Repository) Latest(ctx context.Context, db DBTX, versionID string) (Certification, error) {
	return r.scanCertification(db.QueryRow(ctx, certificationSelect+` WHERE scenario_version_id=$1 ORDER BY created_at DESC, id DESC LIMIT 1`, versionID))
}

// History returns attempts newest-first.
func (r *Repository) History(ctx context.Context, db DBTX, versionID string, limit int) ([]Certification, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := db.Query(ctx, certificationSelect+` WHERE scenario_version_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, versionID, limit)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []Certification{}
	for rows.Next() {
		item, err := scanCertificationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

const certificationSelect = `SELECT id, workspace_id, scenario_id, scenario_version_id, state, certification_contract_version,
	reference_remediation_contract_version, reference_content_hash, evaluator_version,
	runtime_contract_version, environment_profile, provider_id, provider_contract_version,
	isolation_tier, environment_profile_contract_version, environment_component_contract_version,
	criteria_version, hidden_variant_contract_version,
	task_family_id, case_type, gate_results, failure_codes, reference_evaluation_id,
	variant_ids, variant_seeds, idempotency_key, started_at, completed_at FROM scenario_certifications`

func (r *Repository) scanCertification(row pgx.Row) (Certification, error) {
	var gates []byte
	var out Certification
	err := row.Scan(&out.ID, &out.WorkspaceID, &out.ScenarioID, &out.ScenarioVersionID, &out.State, &out.CertificationContractVersion,
		&out.ReferenceRemediationContractVersion, &out.ReferenceContentHash, &out.EvaluatorVersion,
		&out.RuntimeContractVersion, &out.EnvironmentProfile, &out.ProviderID, &out.ProviderContractVersion,
		&out.IsolationTier, &out.EnvironmentProfileContractVersion, &out.EnvironmentComponentContractVersion,
		&out.CriteriaVersion, &out.HiddenVariantContractVersion,
		&out.TaskFamilyID, &out.CaseType, &gates, &out.FailureCodes, &out.ReferenceEvaluationID,
		&out.VariantIDs, &out.VariantSeeds, &out.IdempotencyKey, &out.StartedAt, &out.CompletedAt)
	if err != nil {
		return out, mapDB(err)
	}
	if err := json.Unmarshal(gates, &out.GateResults); err != nil {
		return out, err
	}
	if out.GateResults == nil {
		out.GateResults = []GateResult{}
	}
	if out.FailureCodes == nil {
		out.FailureCodes = []string{}
	}
	if out.VariantIDs == nil {
		out.VariantIDs = []string{}
	}
	if out.VariantSeeds == nil {
		out.VariantSeeds = []int64{}
	}
	if out.TaskFamilyID == "" {
		out.TaskFamilyID = ""
	}
	return out, nil
}

type scannable interface{ Scan(...any) error }

func scanCertificationRow(row scannable) (Certification, error) {
	var gates []byte
	var out Certification
	err := row.Scan(&out.ID, &out.WorkspaceID, &out.ScenarioID, &out.ScenarioVersionID, &out.State, &out.CertificationContractVersion,
		&out.ReferenceRemediationContractVersion, &out.ReferenceContentHash, &out.EvaluatorVersion,
		&out.RuntimeContractVersion, &out.EnvironmentProfile, &out.ProviderID, &out.ProviderContractVersion,
		&out.IsolationTier, &out.EnvironmentProfileContractVersion, &out.EnvironmentComponentContractVersion,
		&out.CriteriaVersion, &out.HiddenVariantContractVersion,
		&out.TaskFamilyID, &out.CaseType, &gates, &out.FailureCodes, &out.ReferenceEvaluationID,
		&out.VariantIDs, &out.VariantSeeds, &out.IdempotencyKey, &out.StartedAt, &out.CompletedAt)
	if err != nil {
		return out, mapDB(err)
	}
	if err := json.Unmarshal(gates, &out.GateResults); err != nil {
		return out, err
	}
	if out.GateResults == nil {
		out.GateResults = []GateResult{}
	}
	if out.FailureCodes == nil {
		out.FailureCodes = []string{}
	}
	if out.VariantIDs == nil {
		out.VariantIDs = []string{}
	}
	if out.VariantSeeds == nil {
		out.VariantSeeds = []int64{}
	}
	return out, nil
}

func nullableStr(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
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
