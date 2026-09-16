-- Scenario families & capability coverage (V1).
--
-- Task families are server-owned with stable dotted IDs. The Go coverage
-- package owns the V1 catalog (task-family-contract:v1); this table pins the
-- IDs so ScenarioVersion assignments are referential and unknown families
-- are rejected at the database boundary too.
CREATE TABLE task_families (
    family_id TEXT PRIMARY KEY CHECK (char_length(family_id) BETWEEN 1 AND 200),
    domain TEXT NOT NULL CHECK (char_length(domain) BETWEEN 1 AND 100),
    label TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 200),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 2000),
    contract_version TEXT NOT NULL DEFAULT 'task-family-contract:v1'
        CHECK (contract_version = 'task-family-contract:v1')
);

INSERT INTO task_families (family_id, domain, label, description) VALUES
    ('kubernetes.workload.baseline', 'Kubernetes', 'Workload baseline', 'Independent workload health situations requiring diagnosis or bounded remediation.'),
    ('kubernetes.capacity.traffic_spike', 'Kubernetes', 'Capacity traffic spike', 'Sudden traffic saturation requiring capacity diagnosis or scaling remediation. Runnable today via the traffic_spike runtime.'),
    ('kubernetes.capacity.resource_pressure', 'Kubernetes', 'Capacity resource pressure', 'Independent capacity pressure situation distinct from a traffic spike.'),
    ('kubernetes.deployment.recovery', 'Kubernetes', 'Deployment recovery', 'Failed or stalled deployment recovery situations.'),
    ('kubernetes.networking.dependency', 'Kubernetes', 'Networking and dependency', 'Service networking symptoms and cross-service dependency faults.'),
    ('observability.metrics.diagnosis', 'Observability', 'Metrics diagnosis', 'Incident diagnosis from request, error, and latency telemetry.'),
    ('observability.telemetry.missing_data', 'Observability', 'Missing telemetry reasoning', 'Situations with absent telemetry where the agent must reason explicitly instead of guessing.'),
    ('observability.recovery.verification', 'Observability', 'Recovery verification', 'Verify recovery through observation before stopping.'),
    ('gitops.drift.deployment', 'GitOps', 'Drift deployment', 'Desired-versus-live drift in deployment state.'),
    ('gitops.desired_state.remediation', 'GitOps', 'Desired state remediation', 'Desired-state mismatch remediation within declared scope.'),
    ('gitops.argocd.recovery', 'GitOps', 'Argo CD recovery', 'Argo CD application health and sync fault recovery.'),
    ('delivery.rollout.recovery', 'Delivery', 'Rollout recovery', 'Stalled or failing rollout recovery situations.'),
    ('delivery.progressive.error_regression', 'Delivery', 'Progressive delivery error regression', 'Progressive-delivery faults visible in error telemetry.'),
    ('safety.boundary.adherence', 'Safety', 'Boundary adherence', 'Situations exercising declared operational scope boundaries.'),
    ('safety.protected_resource.avoidance', 'Safety', 'Protected-resource avoidance', 'Situations tempting action on protected resources.'),
    ('safety.stop_after_recovery', 'Safety', 'Stop after recovery', 'Post-recovery situations where the agent must stop changing state.');

-- Explicit ScenarioVersion -> task family assignment with an explicit case
-- type. One row per ScenarioVersion. Historical ScenarioVersions are never
-- rewritten; the assignment is set once and frozen once evaluation evidence
-- exists (enforced in the service layer with a database backstop below).
CREATE TABLE scenario_version_task_family (
    scenario_version_id UUID PRIMARY KEY REFERENCES scenario_versions(id) ON DELETE CASCADE,
    task_family_id TEXT NOT NULL REFERENCES task_families(family_id) ON DELETE RESTRICT,
    case_type TEXT NOT NULL CHECK (case_type IN ('positive', 'negative', 'decoy', 'adversarial', 'edge')),
    contract_version TEXT NOT NULL DEFAULT 'task-family-contract:v1'
        CHECK (contract_version = 'task-family-contract:v1'),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_by UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX scenario_version_task_family_family_idx
    ON scenario_version_task_family (task_family_id, scenario_version_id);

-- Backstop: forbid silent reassignment once evaluation evidence references
-- the ScenarioVersion (runs via evaluation_batches, or suite execution links).
CREATE FUNCTION reject_task_family_reassignment_after_evidence() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    evidence_count INTEGER;
BEGIN
    IF OLD.task_family_id IS NOT DISTINCT FROM NEW.task_family_id
       AND OLD.case_type IS NOT DISTINCT FROM NEW.case_type THEN
        RETURN NEW;
    END IF;
    SELECT COUNT(*) INTO evidence_count FROM (
        SELECT 1 FROM evaluation_batches WHERE scenario_version_id = OLD.scenario_version_id LIMIT 1
        UNION ALL
        SELECT 1 FROM suite_execution_batches WHERE scenario_version_id = OLD.scenario_version_id LIMIT 1
    ) evidence;
    IF evidence_count > 0 THEN
        RAISE EXCEPTION 'task family assignment is immutable once evaluation evidence exists for this ScenarioVersion' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER scenario_version_task_family_evidence_freeze
BEFORE UPDATE ON scenario_version_task_family
FOR EACH ROW EXECUTE FUNCTION reject_task_family_reassignment_after_evidence();

-- Persisted deterministic coverage assessments. One row per
-- (agent_version, capability, suite_execution, contract versions): exact
-- evidence scope, exact claim, exact contracts. Idempotent creation uses the
-- unique scope constraint with ON CONFLICT DO NOTHING then SELECT.
CREATE TABLE capability_coverage_assessments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE CASCADE,
    capability_id TEXT NOT NULL CHECK (char_length(capability_id) BETWEEN 1 AND 200),
    suite_execution_id UUID NOT NULL REFERENCES suite_executions(id) ON DELETE CASCADE,
    capability_catalog_version TEXT NOT NULL DEFAULT 'infrastructure-capability-catalog:v1'
        CHECK (capability_catalog_version = 'infrastructure-capability-catalog:v1'),
    task_family_contract_version TEXT NOT NULL DEFAULT 'task-family-contract:v1'
        CHECK (task_family_contract_version = 'task-family-contract:v1'),
    coverage_contract_version TEXT NOT NULL DEFAULT 'capability-coverage-contract:v1'
        CHECK (coverage_contract_version = 'capability-coverage-contract:v1'),
    test_status TEXT NOT NULL CHECK (test_status IN ('untested', 'partial', 'tested')),
    required_family_count INTEGER NOT NULL CHECK (required_family_count >= 0),
    covered_family_count INTEGER NOT NULL CHECK (covered_family_count >= 0),
    valid_trial_count INTEGER NOT NULL CHECK (valid_trial_count >= 0),
    distinct_variant_count INTEGER NOT NULL CHECK (distinct_variant_count >= 0),
    families JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(families) = 'array'),
    missing_requirements JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(missing_requirements) = 'array'),
    scenario_version_ids UUID[] NOT NULL DEFAULT '{}',
    run_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_version_id, capability_id, suite_execution_id, coverage_contract_version, task_family_contract_version, capability_catalog_version)
);

CREATE INDEX capability_coverage_assessments_scope_idx
    ON capability_coverage_assessments (workspace_id, agent_version_id, suite_execution_id);
CREATE INDEX capability_coverage_assessments_capability_idx
    ON capability_coverage_assessments (capability_id, agent_version_id);
