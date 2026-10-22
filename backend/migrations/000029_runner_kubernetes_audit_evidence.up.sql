-- Disposable CI runner sources are isolated from customer collector registrations
-- and from customer_action_evidence. This table must never be used for customer ingestion.
CREATE TABLE runner_audit_test_registrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    cluster_identity TEXT NOT NULL CHECK (char_length(cluster_identity) BETWEEN 1 AND 200),
    namespaces TEXT[] NOT NULL CHECK (cardinality(namespaces) BETWEEN 1 AND 100),
    resource_kinds TEXT[] NOT NULL CHECK (cardinality(resource_kinds) BETWEEN 1 AND 20),
    collector_identity TEXT NOT NULL CHECK (collector_identity LIKE 'github-actions:%' AND char_length(collector_identity) <= 256),
    trust_policy TEXT NOT NULL DEFAULT 'runner_bound_test_source' CHECK (trust_policy = 'runner_bound_test_source'),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, id, cluster_identity),
    UNIQUE (workspace_id, cluster_identity, collector_identity)
);

CREATE TABLE runner_audit_test_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    registration_id UUID NOT NULL,
    run_id UUID NOT NULL DEFAULT gen_random_uuid(),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','completed','revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, registration_id) REFERENCES runner_audit_test_registrations(workspace_id, id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, registration_id, id, run_id)
);

CREATE TABLE runner_kubernetes_audit_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    registration_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    cluster_identity TEXT NOT NULL,
    source_event_id TEXT NOT NULL CHECK (char_length(source_event_id) BETWEEN 1 AND 256),
    source_event_digest TEXT NOT NULL CHECK (source_event_digest ~ '^sha256:[0-9a-f]{64}$'),
    payload_digest TEXT NOT NULL CHECK (payload_digest ~ '^sha256:[0-9a-f]{64}$'),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, registration_id, session_id, run_id)
        REFERENCES runner_audit_test_sessions(workspace_id, registration_id, id, run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, registration_id, cluster_identity)
        REFERENCES runner_audit_test_registrations(workspace_id, id, cluster_identity) ON DELETE RESTRICT,
    -- A source event is consumed once per tenant and registered cluster, across sessions.
    UNIQUE (workspace_id, cluster_identity, source_event_id),
    UNIQUE (session_id, source_event_id)
);
CREATE INDEX runner_kubernetes_audit_evidence_scope_idx
    ON runner_kubernetes_audit_evidence(workspace_id, registration_id, session_id, ingested_at);
CREATE FUNCTION prevent_runner_kubernetes_audit_evidence_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'runner Kubernetes audit evidence is immutable';
END;
$$;
CREATE TRIGGER runner_kubernetes_audit_evidence_immutable
BEFORE UPDATE OR DELETE ON runner_kubernetes_audit_evidence
FOR EACH ROW EXECUTE FUNCTION prevent_runner_kubernetes_audit_evidence_update();
CREATE FUNCTION prevent_runner_kubernetes_audit_evidence_truncate()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM runner_kubernetes_audit_evidence) THEN
        RAISE EXCEPTION 'runner Kubernetes audit evidence is immutable';
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER runner_kubernetes_audit_evidence_no_truncate
BEFORE TRUNCATE ON runner_kubernetes_audit_evidence
FOR EACH STATEMENT EXECUTE FUNCTION prevent_runner_kubernetes_audit_evidence_truncate();
COMMENT ON TABLE runner_kubernetes_audit_evidence IS
    'Append-only CI fixture evidence. Runner binding is insufficient provenance and never authorizes customer evidence ingestion or agent attribution.';
