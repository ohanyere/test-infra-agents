CREATE TABLE evaluation_suites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE INDEX evaluation_suites_workspace_created_idx ON evaluation_suites (workspace_id, created_at DESC);

CREATE TABLE evaluation_suite_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suite_id UUID NOT NULL REFERENCES evaluation_suites(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL CHECK (version > 0),
    description TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (suite_id, version)
);

CREATE TABLE evaluation_suite_version_scenarios (
    suite_version_id UUID NOT NULL REFERENCES evaluation_suite_versions(id) ON DELETE RESTRICT,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    trial_count INTEGER NOT NULL CHECK (trial_count BETWEEN 1 AND 20),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    PRIMARY KEY (suite_version_id, scenario_version_id),
    UNIQUE (suite_version_id, sort_order)
);

CREATE INDEX evaluation_suite_version_scenarios_scenario_idx ON evaluation_suite_version_scenarios (scenario_version_id);

CREATE FUNCTION reject_evaluation_suite_version_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'evaluation suite versions are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER evaluation_suite_version_definition_immutable
BEFORE UPDATE OF suite_id, version, description, metadata ON evaluation_suite_versions
FOR EACH ROW EXECUTE FUNCTION reject_evaluation_suite_version_mutation();

CREATE TRIGGER evaluation_suite_versions_delete_immutable
BEFORE DELETE ON evaluation_suite_versions
FOR EACH ROW EXECUTE FUNCTION reject_evaluation_suite_version_mutation();

CREATE TRIGGER evaluation_suite_version_scenarios_immutable
BEFORE UPDATE OR DELETE ON evaluation_suite_version_scenarios
FOR EACH ROW EXECUTE FUNCTION reject_evaluation_suite_version_mutation();

CREATE TABLE suite_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    suite_version_id UUID NOT NULL REFERENCES evaluation_suite_versions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','failed','stopped')),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX suite_executions_workspace_created_idx ON suite_executions (workspace_id, created_at DESC);

CREATE TABLE suite_execution_agents (
    suite_execution_id UUID NOT NULL REFERENCES suite_executions(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    PRIMARY KEY (suite_execution_id, agent_version_id)
);

CREATE TABLE suite_execution_batches (
    suite_execution_id UUID NOT NULL REFERENCES suite_executions(id) ON DELETE CASCADE,
    batch_id UUID NOT NULL UNIQUE REFERENCES evaluation_batches(id) ON DELETE RESTRICT,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    PRIMARY KEY (suite_execution_id, scenario_version_id)
);

CREATE INDEX suite_execution_batches_batch_idx ON suite_execution_batches (batch_id);
