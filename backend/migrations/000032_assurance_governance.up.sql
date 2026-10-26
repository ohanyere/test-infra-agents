-- Existing versions keep their original active flag; new versions also record
-- an explicit governance state. History rows are never rewritten.
ALTER TABLE operational_contract_versions ADD COLUMN governance_state TEXT
    CHECK (governance_state IN ('draft','active','inactive'));

CREATE TABLE operational_approval_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    action_id TEXT NOT NULL CHECK (char_length(action_id) BETWEEN 1 AND 200),
    decision TEXT NOT NULL CHECK (decision IN ('approved','denied')),
    resource TEXT NOT NULL CHECK (char_length(resource) BETWEEN 1 AND 256),
    approved_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL DEFAULT '',
    valid_until TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,session_id,action_id,approved_by,recorded_at)
);
CREATE INDEX operational_approval_scope_idx ON operational_approval_records(workspace_id,environment_id,session_id,action_id,recorded_at);
CREATE TRIGGER operational_approval_records_immutable BEFORE UPDATE OR DELETE ON operational_approval_records
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_approval_records_no_truncate BEFORE TRUNCATE ON operational_approval_records
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_finding_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    report_revision INTEGER NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    contract_id UUID,
    contract_version INTEGER,
    disposition TEXT NOT NULL CHECK (disposition IN ('violation','candidate','inconclusive','recovered_provisional')),
    dimension TEXT NOT NULL,
    requirement TEXT NOT NULL,
    affected_resource TEXT NOT NULL DEFAULT '',
    provenance_status TEXT NOT NULL,
    attribution_status TEXT NOT NULL,
    finding JSONB NOT NULL CHECK (jsonb_typeof(finding) = 'object'),
    evaluated_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,run_id,report_revision)
        REFERENCES operational_assessment_reports(workspace_id,run_id,revision) ON DELETE RESTRICT,
    UNIQUE (workspace_id,run_id,report_revision,ordinal)
);
CREATE INDEX operational_finding_scope_idx ON operational_finding_history(workspace_id,environment_id,session_id,report_revision DESC);
CREATE TRIGGER operational_finding_history_immutable BEFORE UPDATE OR DELETE ON operational_finding_history
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_finding_history_no_truncate BEFORE TRUNCATE ON operational_finding_history
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE assurance_notification_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    finding_id UUID NOT NULL REFERENCES operational_finding_history(id) ON DELETE RESTRICT,
    event_kind TEXT NOT NULL CHECK (event_kind IN ('confirmed_violation','candidate_violation','evidence_unavailable','stability_regression','reconciliation_failure')),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (finding_id,event_kind),
    FOREIGN KEY (workspace_id,environment_id,session_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE INDEX assurance_notification_scope_idx ON assurance_notification_events(workspace_id,environment_id,session_id,created_at DESC);
CREATE TRIGGER assurance_notification_events_immutable BEFORE UPDATE OR DELETE ON assurance_notification_events
FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER assurance_notification_events_no_truncate BEFORE TRUNCATE ON assurance_notification_events
FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE assurance_notification_deliveries (
    event_id UUID PRIMARY KEY REFERENCES assurance_notification_events(id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','inflight','delivered','failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE assurance_evaluation_jobs (
    session_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    run_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','completed','failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_fingerprint TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT
);
CREATE INDEX assurance_evaluation_due_idx ON assurance_evaluation_jobs(state,next_attempt_at);
