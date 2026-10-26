-- Authenticated transport does not authenticate the audit record's API-server origin.
CREATE TABLE customer_audit_source_registrations (
    environment_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind = 'collector_forwarded_audit_unverified'),
    cluster_binding_status TEXT NOT NULL CHECK (cluster_binding_status = 'registered_unverified'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id)
);
CREATE TABLE customer_kubernetes_audit_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    collector_credential_version BIGINT NOT NULL CHECK (collector_credential_version > 0),
    registered_cluster_identity TEXT NOT NULL,
    source_event_id TEXT NOT NULL CHECK (char_length(source_event_id) BETWEEN 1 AND 256),
    source_event_digest TEXT NOT NULL CHECK (source_event_digest ~ '^sha256:[0-9a-f]{64}$'),
    operation TEXT NOT NULL CHECK (operation IN ('patch','update')),
    resource_kind TEXT NOT NULL CHECK (resource_kind = 'Deployment'),
    resource_namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    resource_uid TEXT NOT NULL,
    kubernetes_principal TEXT NOT NULL DEFAULT '',
    request_received_at TIMESTAMPTZ NOT NULL,
    stage_at TIMESTAMPTZ NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    collector_authentication_status TEXT NOT NULL CHECK (collector_authentication_status = 'verified'),
    audit_origin_status TEXT NOT NULL CHECK (audit_origin_status = 'unverified'),
    trust_classification TEXT NOT NULL CHECK (trust_classification = 'INSUFFICIENT_PROVENANCE'),
    event_payload JSONB NOT NULL CHECK (jsonb_typeof(event_payload) = 'object'),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_audit_source_registrations(workspace_id,environment_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,source_event_id)
);
CREATE INDEX customer_kubernetes_audit_records_scope_idx ON customer_kubernetes_audit_records(workspace_id,environment_id,session_id,persisted_at);
CREATE FUNCTION prevent_customer_kubernetes_audit_record_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'customer Kubernetes audit records are immutable';
END;
$$;
CREATE TRIGGER customer_kubernetes_audit_records_immutable BEFORE UPDATE OR DELETE ON customer_kubernetes_audit_records
FOR EACH ROW EXECUTE FUNCTION prevent_customer_kubernetes_audit_record_change();
CREATE FUNCTION prevent_customer_audit_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'customer audit history is immutable';
END;
$$;
CREATE TRIGGER customer_kubernetes_audit_records_no_truncate BEFORE TRUNCATE ON customer_kubernetes_audit_records
FOR EACH STATEMENT EXECUTE FUNCTION prevent_customer_audit_truncate();

-- Mutable case is a recovery cursor; its history is append-only and versioned.
CREATE TABLE customer_audit_reconciliation_cases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    claim_id UUID NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('expected','acquiring','received','validated','unavailable','rejected','reconciliation_pending','reconciliation_completed')),
    result TEXT NOT NULL DEFAULT 'none' CHECK (result IN ('none','missing','insufficient','conflicting')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    deadline_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    lease_until TIMESTAMPTZ,
    lease_token UUID,
    evaluated_at TIMESTAMPTZ,
    last_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (session_id,claim_id) REFERENCES customer_agent_action_claims(session_id,claim_id) ON DELETE RESTRICT,
    UNIQUE (session_id,claim_id)
);
CREATE INDEX customer_audit_reconciliation_due_idx ON customer_audit_reconciliation_cases(next_attempt_at,id)
WHERE state <> 'reconciliation_completed';
CREATE TABLE customer_audit_reconciliation_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES customer_audit_reconciliation_cases(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision > 0),
    state TEXT NOT NULL CHECK (state IN ('expected','acquiring','received','validated','unavailable','rejected','reconciliation_pending','reconciliation_completed')),
    result TEXT NOT NULL CHECK (result IN ('none','missing','insufficient','conflicting')),
    reason TEXT NOT NULL,
    audit_record_id UUID REFERENCES customer_kubernetes_audit_records(id) ON DELETE RESTRICT,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (case_id,revision)
);
CREATE TRIGGER customer_audit_reconciliation_events_immutable BEFORE UPDATE OR DELETE ON customer_audit_reconciliation_events
FOR EACH ROW EXECUTE FUNCTION prevent_customer_kubernetes_audit_record_change();
CREATE TRIGGER customer_audit_reconciliation_events_no_truncate BEFORE TRUNCATE ON customer_audit_reconciliation_events
FOR EACH STATEMENT EXECUTE FUNCTION prevent_customer_audit_truncate();
