-- Direct API-server audit webhook credentials and independent execution issuers.
-- A registered credential authenticates its holder. Operational deployment
-- must keep the webhook client key confined to the API server and issuer key
-- confined to the customer execution gateway.
ALTER TABLE customer_action_evidence ADD CONSTRAINT customer_action_evidence_trusted_scope_key UNIQUE (workspace_id,environment_id,session_id,run_id,id);
CREATE TABLE customer_audit_mtls_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    certificate_sha256 BYTEA NOT NULL UNIQUE CHECK (octet_length(certificate_sha256)=32),
    certificate_subject TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '90 days'),
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX customer_audit_mtls_sources_scope_idx ON customer_audit_mtls_sources(workspace_id,environment_id);

CREATE TABLE customer_authenticated_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    source_id UUID NOT NULL,
    source_event_id TEXT NOT NULL,
    source_event_digest BYTEA NOT NULL CHECK (octet_length(source_event_digest)=32),
    evidence_id UUID NOT NULL,
    event_raw BYTEA NOT NULL,
    event_payload JSONB NOT NULL CHECK (jsonb_typeof(event_payload)='object'),
    event_at TIMESTAMPTZ NOT NULL,
    source_observed_at TIMESTAMPTZ NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,source_id) REFERENCES customer_audit_mtls_sources(workspace_id,environment_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id,evidence_id) REFERENCES customer_action_evidence(workspace_id,environment_id,session_id,run_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,source_event_id),
    UNIQUE (workspace_id,environment_id,session_id,run_id,id),
    UNIQUE (evidence_id)
);
CREATE INDEX customer_authenticated_audit_events_scope_idx ON customer_authenticated_audit_events(workspace_id,environment_id,session_id,persisted_at);

CREATE TABLE customer_execution_issuers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    agent_certificate_sha256 BYTEA NOT NULL CHECK (octet_length(agent_certificate_sha256)=32),
    public_key BYTEA NOT NULL UNIQUE CHECK (octet_length(public_key)=32),
    credential_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(credential_hash)=32),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '90 days'),
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,id),
    UNIQUE (workspace_id,environment_id,id,agent_version_id,agent_certificate_sha256)
);

CREATE TABLE customer_execution_receipts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    issuer_id UUID NOT NULL,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    agent_certificate_sha256 BYTEA NOT NULL CHECK (octet_length(agent_certificate_sha256)=32),
    action_id UUID NOT NULL,
    execution_session_id UUID NOT NULL,
    source_event_id TEXT NOT NULL,
    kubernetes_principal TEXT NOT NULL,
    operation TEXT NOT NULL,
    resource_namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    resource_uid TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    payload_digest BYTEA NOT NULL CHECK (octet_length(payload_digest)=32),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload)='object'),
    signature BYTEA NOT NULL CHECK (octet_length(signature)=64),
    authenticated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,issuer_id,agent_version_id,agent_certificate_sha256) REFERENCES customer_execution_issuers(workspace_id,environment_id,id,agent_version_id,agent_certificate_sha256) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,session_id,run_id,id),
    UNIQUE (issuer_id,action_id),
    UNIQUE (issuer_id,source_event_id)
);
CREATE INDEX customer_execution_receipts_session_idx ON customer_execution_receipts(workspace_id,environment_id,session_id);

CREATE TABLE customer_attribution_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    audit_event_id UUID NOT NULL,
    execution_receipt_id UUID,
    status TEXT NOT NULL CHECK (status IN ('ATTRIBUTED','NOT_ATTRIBUTED','INCONCLUSIVE')),
    reason_code TEXT NOT NULL,
    reason TEXT NOT NULL,
    decision_digest BYTEA NOT NULL CHECK (octet_length(decision_digest)=32),
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id,audit_event_id) REFERENCES customer_authenticated_audit_events(workspace_id,environment_id,session_id,run_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id,execution_receipt_id) REFERENCES customer_execution_receipts(workspace_id,environment_id,session_id,run_id,id) ON DELETE RESTRICT,
    UNIQUE (audit_event_id,decision_digest)
);
CREATE INDEX customer_attribution_decisions_scope_idx ON customer_attribution_decisions(workspace_id,environment_id,session_id,evaluated_at);

CREATE FUNCTION prevent_customer_trust_history_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'customer trust evidence and decisions are immutable';
END;
$$;
CREATE TRIGGER customer_authenticated_audit_events_immutable BEFORE UPDATE OR DELETE ON customer_authenticated_audit_events FOR EACH ROW EXECUTE FUNCTION prevent_customer_trust_history_change();
CREATE TRIGGER customer_execution_receipts_immutable BEFORE UPDATE OR DELETE ON customer_execution_receipts FOR EACH ROW EXECUTE FUNCTION prevent_customer_trust_history_change();
CREATE TRIGGER customer_attribution_decisions_immutable BEFORE UPDATE OR DELETE ON customer_attribution_decisions FOR EACH ROW EXECUTE FUNCTION prevent_customer_trust_history_change();
