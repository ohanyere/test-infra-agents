-- Qualification plans (automatic qualification suite builder).
--
-- A QualificationPlan is the server-derived answer to "what must be tested
-- to determine whether these claims can be qualified?" for one exact
-- AgentVersion. State, selections, trial counts, and blockers are all
-- server-derived; callers supply only the AgentVersion and an optional
-- claimed-capability subset. Plans are immutable once written; suite links
-- live in a separate mapping table so builds never mutate a plan.
CREATE TABLE qualification_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    capability_ids TEXT[] NOT NULL DEFAULT '{}',
    coverage_contract_version TEXT NOT NULL DEFAULT 'capability-coverage-contract:v2'
        CHECK (coverage_contract_version IN ('capability-coverage-contract:v1', 'capability-coverage-contract:v2')),
    task_family_contract_version TEXT NOT NULL DEFAULT 'task-family-contract:v1'
        CHECK (task_family_contract_version = 'task-family-contract:v1'),
    capability_catalog_version TEXT NOT NULL DEFAULT 'infrastructure-capability-catalog:v1'
        CHECK (capability_catalog_version = 'infrastructure-capability-catalog:v1'),
    planner_contract_version TEXT NOT NULL DEFAULT 'qualification-suite-planner:v1'
        CHECK (planner_contract_version = 'qualification-suite-planner:v1'),
    certification_contract_version TEXT NOT NULL DEFAULT 'scenario-certification-contract:v1'
        CHECK (certification_contract_version = 'scenario-certification-contract:v1'),
    evaluator_version TEXT NOT NULL DEFAULT 'deterministic-evaluator:v1'
        CHECK (evaluator_version = 'deterministic-evaluator:v1'),
    environment_profile_contract_version TEXT NOT NULL DEFAULT 'environment-profile-catalog:v1',
    state TEXT NOT NULL CHECK (state IN ('ready', 'partial', 'blocked')),
    plan JSONB NOT NULL CHECK (jsonb_typeof(plan) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX qualification_plans_workspace_version_idx
    ON qualification_plans (workspace_id, agent_version_id, created_at DESC);

CREATE FUNCTION reject_qualification_plan_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'qualification plans are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER qualification_plans_immutable
BEFORE UPDATE OR DELETE ON qualification_plans
FOR EACH ROW EXECUTE FUNCTION reject_qualification_plan_mutation();

-- Suite builds from READY plans. One authoritative suite per plan; the link
-- is a separate row so the plan itself is never mutated.
CREATE TABLE qualification_plan_suites (
    plan_id UUID PRIMARY KEY REFERENCES qualification_plans(id) ON DELETE CASCADE,
    suite_id UUID NOT NULL REFERENCES evaluation_suites(id) ON DELETE RESTRICT,
    suite_version_id UUID NOT NULL REFERENCES evaluation_suite_versions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
