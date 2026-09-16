ALTER TABLE evaluation_suite_version_scenarios
  ADD COLUMN hidden_variants JSONB NOT NULL DEFAULT '[]'::jsonb
  CHECK (jsonb_typeof(hidden_variants) = 'array');

CREATE TABLE suite_execution_run_variants (
    run_id UUID PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    suite_execution_id UUID NOT NULL REFERENCES suite_executions(id) ON DELETE CASCADE,
    suite_version_id UUID NOT NULL REFERENCES evaluation_suite_versions(id) ON DELETE RESTRICT,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    trial_ordinal INTEGER NOT NULL CHECK (trial_ordinal BETWEEN 1 AND 20),
    variant_id TEXT NOT NULL CHECK (length(variant_id) BETWEEN 1 AND 100),
    variant_seed BIGINT NOT NULL CHECK (variant_seed >= 0),
    hidden_config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(hidden_config) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (suite_execution_id, scenario_version_id, agent_version_id, trial_ordinal)
);

CREATE INDEX suite_execution_run_variants_execution_idx
  ON suite_execution_run_variants (suite_execution_id, agent_version_id, scenario_version_id, trial_ordinal);

CREATE FUNCTION reject_suite_run_variant_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'suite execution variant assignments are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER suite_execution_run_variants_immutable
BEFORE UPDATE OR DELETE ON suite_execution_run_variants
FOR EACH ROW EXECUTE FUNCTION reject_suite_run_variant_mutation();
