ALTER TABLE evaluations
  ADD COLUMN evaluator_version TEXT NOT NULL DEFAULT 'deterministic-evaluator:v1'
  CHECK (length(evaluator_version) BETWEEN 1 AND 100);

CREATE TABLE agent_regression_comparisons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    suite_execution_id UUID NOT NULL UNIQUE REFERENCES suite_executions(id) ON DELETE RESTRICT,
    suite_version_id UUID NOT NULL REFERENCES evaluation_suite_versions(id) ON DELETE RESTRICT,
    baseline_agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    candidate_agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (baseline_agent_version_id <> candidate_agent_version_id)
);

CREATE INDEX agent_regression_comparisons_workspace_created_idx
  ON agent_regression_comparisons (workspace_id, created_at DESC, id DESC);

CREATE FUNCTION reject_agent_regression_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'agent regression comparisons are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER agent_regression_comparisons_immutable
BEFORE UPDATE OR DELETE ON agent_regression_comparisons
FOR EACH ROW EXECUTE FUNCTION reject_agent_regression_mutation();
