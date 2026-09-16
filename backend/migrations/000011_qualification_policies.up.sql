CREATE TABLE qualification_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE qualification_policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES qualification_policies(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL CHECK (version > 0),
    description TEXT NOT NULL DEFAULT '',
    minimum_valid_trials INTEGER NOT NULL CHECK (minimum_valid_trials BETWEEN 1 AND 10000),
    minimum_overall_pass_rate NUMERIC(6,3) NOT NULL CHECK (minimum_overall_pass_rate BETWEEN 0 AND 100),
    minimum_per_scenario_pass_rate NUMERIC(6,3) CHECK (minimum_per_scenario_pass_rate BETWEEN 0 AND 100),
    maximum_inconclusive_rate NUMERIC(6,3) NOT NULL CHECK (maximum_inconclusive_rate BETWEEN 0 AND 100),
    maximum_invalid_platform_rate NUMERIC(6,3) CHECK (maximum_invalid_platform_rate BETWEEN 0 AND 100),
    maximum_safety_violations INTEGER NOT NULL CHECK (maximum_safety_violations >= 0),
    forbidden_safety_codes TEXT[] NOT NULL DEFAULT '{}',
    require_no_regression BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, version)
);

CREATE FUNCTION reject_qualification_policy_version_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'qualification policy versions are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER qualification_policy_versions_immutable
BEFORE UPDATE OR DELETE ON qualification_policy_versions
FOR EACH ROW EXECUTE FUNCTION reject_qualification_policy_version_mutation();

CREATE TABLE qualification_assessments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    policy_version_id UUID NOT NULL REFERENCES qualification_policy_versions(id) ON DELETE RESTRICT,
    suite_execution_id UUID NOT NULL REFERENCES suite_executions(id) ON DELETE RESTRICT,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    regression_comparison_id UUID REFERENCES agent_regression_comparisons(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('qualified','not_qualified','inconclusive')),
    evidence_snapshot JSONB NOT NULL CHECK (jsonb_typeof(evidence_snapshot) = 'object'),
    reproducibility_manifest JSONB NOT NULL CHECK (jsonb_typeof(reproducibility_manifest) = 'array'),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX qualification_assessments_without_regression_uidx
  ON qualification_assessments(policy_version_id,suite_execution_id,agent_version_id)
  WHERE regression_comparison_id IS NULL;
CREATE UNIQUE INDEX qualification_assessments_with_regression_uidx
  ON qualification_assessments(policy_version_id,suite_execution_id,agent_version_id,regression_comparison_id)
  WHERE regression_comparison_id IS NOT NULL;

CREATE TABLE qualification_rule_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    assessment_id UUID NOT NULL REFERENCES qualification_assessments(id) ON DELETE RESTRICT,
    rule_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('passed','failed','inconclusive')),
    expected JSONB NOT NULL,
    observed JSONB NOT NULL,
    reason TEXT NOT NULL,
    supporting_references TEXT[] NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (assessment_id, rule_type, sort_order)
);

CREATE FUNCTION reject_qualification_assessment_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'qualification assessments are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER qualification_assessments_immutable
BEFORE UPDATE OR DELETE ON qualification_assessments
FOR EACH ROW EXECUTE FUNCTION reject_qualification_assessment_mutation();
CREATE TRIGGER qualification_rule_results_immutable
BEFORE UPDATE OR DELETE ON qualification_rule_results
FOR EACH ROW EXECUTE FUNCTION reject_qualification_assessment_mutation();
