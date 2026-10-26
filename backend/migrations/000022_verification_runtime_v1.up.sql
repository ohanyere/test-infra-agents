-- Authoritative, immutable verification projection over existing run evidence.
-- Historical runs are intentionally not backfilled.
ALTER TABLE evaluation_batches
    ADD COLUMN source_metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_metadata) = 'object'),
    ADD COLUMN idempotency_key TEXT CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 200);
CREATE UNIQUE INDEX evaluation_batches_ci_idempotency_idx
    ON evaluation_batches(workspace_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE verification_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    run_id UUID NOT NULL UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    environment_instance_id UUID REFERENCES environment_instances(id) ON DELETE SET NULL,
    composition_plan_id UUID REFERENCES composition_plans(id) ON DELETE RESTRICT,
    composition_plan_hash TEXT CHECK (composition_plan_hash IS NULL OR composition_plan_hash ~ '^sha256:[0-9a-f]{64}$'),
    verifier_contract_version TEXT NOT NULL CHECK (verifier_contract_version = 'infrastructure-verifier:v1'),
    snapshot_contract_version TEXT NOT NULL CHECK (snapshot_contract_version = 'kubernetes-snapshot:v1'),
    baseline_snapshot JSONB NOT NULL CHECK (jsonb_typeof(baseline_snapshot) = 'object'),
    final_snapshot JSONB NOT NULL CHECK (jsonb_typeof(final_snapshot) = 'object'),
    intent_result JSONB NOT NULL CHECK (jsonb_typeof(intent_result) = 'object'),
    action_inspection JSONB NOT NULL CHECK (jsonb_typeof(action_inspection) = 'object'),
    infrastructure_diff JSONB NOT NULL CHECK (jsonb_typeof(infrastructure_diff) = 'array'),
    outcome_result JSONB NOT NULL CHECK (jsonb_typeof(outcome_result) = 'object'),
    side_effect_result JSONB NOT NULL CHECK (jsonb_typeof(side_effect_result) = 'object'),
    stability_result JSONB NOT NULL CHECK (jsonb_typeof(stability_result) = 'object'),
    forward_risk_findings JSONB NOT NULL CHECK (jsonb_typeof(forward_risk_findings) = 'array'),
    platform_validity TEXT NOT NULL CHECK (platform_validity IN ('valid','invalid_platform','invalid_task')),
    overall_status TEXT NOT NULL CHECK (overall_status IN ('VERIFIED','FAILED','WARNING','INCONCLUSIVE')),
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    source_metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_metadata) = 'object'),
    evidence_started_at TIMESTAMPTZ,
    evidence_completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX verification_results_workspace_created_idx ON verification_results(workspace_id, created_at DESC, id DESC);
CREATE INDEX verification_results_agent_version_idx ON verification_results(workspace_id, agent_version_id, created_at DESC);

CREATE FUNCTION prevent_verification_result_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'verification results are immutable'; END; $$;
CREATE TRIGGER verification_results_immutable
    BEFORE UPDATE ON verification_results
    FOR EACH ROW EXECUTE FUNCTION prevent_verification_result_mutation();
