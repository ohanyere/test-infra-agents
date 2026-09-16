-- Scenario certification & task quality gate (V1).
--
-- Certification applies to an exact immutable ScenarioVersion. Historical rows
-- pin the exact certification contract so future rule changes never rewrite
-- history. Reference remediation is EVALUATOR-ONLY and is never exposed
-- through AgentView / gateway / MCP / public scenario APIs.

CREATE TABLE scenario_version_reference_remediations (
    scenario_version_id UUID PRIMARY KEY REFERENCES scenario_versions(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contract_version TEXT NOT NULL DEFAULT 'reference-remediation:v1'
        CHECK (contract_version = 'reference-remediation:v1'),
    preconditions JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(preconditions) = 'object'),
    reference_actions JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(reference_actions) = 'array'),
    expected_resulting_state JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(expected_resulting_state) = 'object'),
    verification_notes TEXT NOT NULL DEFAULT '' CHECK (char_length(verification_notes) <= 4000),
    cleanup_expectations TEXT NOT NULL DEFAULT '' CHECK (char_length(cleanup_expectations) <= 4000),
    content_hash TEXT NOT NULL CHECK (char_length(content_hash) BETWEEN 1 AND 128),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE scenario_certifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scenario_id UUID NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('certifying', 'certified', 'failed')),
    certification_contract_version TEXT NOT NULL DEFAULT 'scenario-certification-contract:v1'
        CHECK (certification_contract_version = 'scenario-certification-contract:v1'),
    reference_remediation_contract_version TEXT NOT NULL DEFAULT 'reference-remediation:v1'
        CHECK (reference_remediation_contract_version = 'reference-remediation:v1'),
    reference_content_hash TEXT NOT NULL DEFAULT '' CHECK (char_length(reference_content_hash) <= 128),
    evaluator_version TEXT NOT NULL DEFAULT 'deterministic-evaluator:v1'
        CHECK (evaluator_version = 'deterministic-evaluator:v1'),
    runtime_contract_version TEXT NOT NULL CHECK (char_length(runtime_contract_version) BETWEEN 1 AND 200),
    environment_profile TEXT NOT NULL DEFAULT 'default' CHECK (char_length(environment_profile) BETWEEN 1 AND 200),
    criteria_version TEXT NOT NULL DEFAULT '' CHECK (char_length(criteria_version) <= 128),
    hidden_variant_contract_version TEXT NOT NULL DEFAULT 'hidden-variant-contract:v1'
        CHECK (hidden_variant_contract_version = 'hidden-variant-contract:v1'),
    task_family_id TEXT,
    case_type TEXT CHECK (case_type IS NULL OR case_type IN ('positive', 'negative', 'decoy', 'adversarial', 'edge')),
    gate_results JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(gate_results) = 'array'),
    failure_codes TEXT[] NOT NULL DEFAULT '{}',
    reference_evaluation_id TEXT,
    variant_ids TEXT[] NOT NULL DEFAULT '{}',
    variant_seeds BIGINT[] NOT NULL DEFAULT '{}',
    idempotency_key TEXT CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 200),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX scenario_certifications_idempotency_uidx
    ON scenario_certifications (scenario_version_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX scenario_certifications_version_idx
    ON scenario_certifications (scenario_version_id, created_at DESC, id DESC);
CREATE INDEX scenario_certifications_workspace_idx
    ON scenario_certifications (workspace_id, scenario_version_id);
