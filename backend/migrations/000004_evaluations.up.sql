CREATE TABLE evaluations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,
    result TEXT NOT NULL CHECK (result IN ('pass', 'fail', 'inconclusive', 'stopped')),
    score NUMERIC(5,2) CHECK (score IS NULL OR (score >= 0 AND score <= 100)),
    summary TEXT NOT NULL DEFAULT '',
    recovery_duration_ms BIGINT CHECK (recovery_duration_ms IS NULL OR recovery_duration_ms >= 0),
    availability_pct NUMERIC(6,3) CHECK (availability_pct IS NULL OR (availability_pct >= 0 AND availability_pct <= 100)),
    peak_error_rate_pct NUMERIC(6,3) CHECK (peak_error_rate_pct IS NULL OR (peak_error_rate_pct >= 0 AND peak_error_rate_pct <= 100)),
    safety_violation_count INTEGER NOT NULL DEFAULT 0 CHECK (safety_violation_count >= 0),
    secondary_incident_count INTEGER NOT NULL DEFAULT 0 CHECK (secondary_incident_count >= 0),
    agent_action_count INTEGER NOT NULL DEFAULT 0 CHECK (agent_action_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE evaluation_criterion_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id UUID NOT NULL REFERENCES evaluations(id) ON DELETE CASCADE,
    scenario_criterion_id UUID NOT NULL REFERENCES scenario_criteria(id),
    passed BOOLEAN NOT NULL,
    observed_value JSONB NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (evaluation_id, scenario_criterion_id)
);

CREATE INDEX evaluation_criterion_results_criterion_id_idx ON evaluation_criterion_results (scenario_criterion_id);

CREATE TABLE evaluation_findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    evaluation_id UUID NOT NULL REFERENCES evaluations(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('strength', 'limitation', 'safety', 'qualification')),
    summary TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence) = 'object'),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX evaluation_findings_evaluation_sort_idx ON evaluation_findings (evaluation_id, sort_order);
