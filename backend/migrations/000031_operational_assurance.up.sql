-- Contract revisions and operational assessments are append-only. They do not
-- change the canonical verification_results verdict.
CREATE TABLE operational_contract_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    service_name TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    rules JSONB NOT NULL CHECK (jsonb_typeof(rules) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,contract_id,version)
);
CREATE INDEX operational_contract_versions_scope_idx ON operational_contract_versions(workspace_id,environment_id,contract_id,version DESC);
CREATE TABLE customer_service_metric_samples (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    sample_id UUID NOT NULL,
    collector_credential_version BIGINT NOT NULL CHECK (collector_credential_version > 0),
    namespace TEXT NOT NULL,
    service_name TEXT NOT NULL,
    sampled_at TIMESTAMPTZ NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    error_rate_pct DOUBLE PRECISION CHECK (error_rate_pct IS NULL OR error_rate_pct BETWEEN 0 AND 100),
    request_success_rate_pct DOUBLE PRECISION CHECK (request_success_rate_pct IS NULL OR request_success_rate_pct BETWEEN 0 AND 100),
    p95_latency_ms DOUBLE PRECISION CHECK (p95_latency_ms IS NULL OR p95_latency_ms >= 0),
    source_kind TEXT NOT NULL CHECK (source_kind = 'collector_forwarded_metrics_unverified'),
    source_digest TEXT NOT NULL CHECK (source_digest ~ '^sha256:[0-9a-f]{64}$'),
    CHECK (error_rate_pct IS NOT NULL OR request_success_rate_pct IS NOT NULL OR p95_latency_ms IS NOT NULL),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,sample_id)
);
CREATE INDEX customer_service_metric_samples_scope_idx ON customer_service_metric_samples(workspace_id,environment_id,session_id,service_name,sampled_at);
CREATE TABLE operational_assessment_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    source_fingerprint TEXT NOT NULL CHECK (source_fingerprint ~ '^sha256:[0-9a-f]{64}$'),
    canonical_result_id UUID NOT NULL REFERENCES verification_results(id) ON DELETE RESTRICT,
    report JSONB NOT NULL CHECK (jsonb_typeof(report) = 'object'),
    evaluated_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,run_id,revision),
    UNIQUE (workspace_id,run_id,source_fingerprint)
);
CREATE INDEX operational_assessment_reports_scope_idx ON operational_assessment_reports(workspace_id,environment_id,session_id,revision DESC);
CREATE FUNCTION prevent_operational_history_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'operational contract and assessment history is immutable';
END;
$$;
CREATE TRIGGER operational_contract_versions_immutable BEFORE UPDATE OR DELETE ON operational_contract_versions
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_assessment_reports_immutable BEFORE UPDATE OR DELETE ON operational_assessment_reports
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER customer_service_metric_samples_immutable BEFORE UPDATE OR DELETE ON customer_service_metric_samples
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_contract_versions_no_truncate BEFORE TRUNCATE ON operational_contract_versions
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_assessment_reports_no_truncate BEFORE TRUNCATE ON operational_assessment_reports
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER customer_service_metric_samples_no_truncate BEFORE TRUNCATE ON customer_service_metric_samples
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();
