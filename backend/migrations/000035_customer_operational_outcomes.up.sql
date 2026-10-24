-- Registered direct read-only source identities and immutable acquired results.
ALTER TABLE customer_kubernetes_environments DROP CONSTRAINT customer_kubernetes_environments_resource_kinds_check;
ALTER TABLE customer_kubernetes_environments ADD CONSTRAINT customer_kubernetes_environments_resource_kinds_check CHECK (cardinality(resource_kinds) BETWEEN 1 AND 6);
CREATE TABLE customer_outcome_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('kubernetes_state','argocd','prometheus')),
    endpoint_url TEXT NOT NULL CHECK (endpoint_url LIKE 'https://%'),
    server_certificate_sha256 BYTEA NOT NULL CHECK (octet_length(server_certificate_sha256)=32),
    namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    configuration JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(configuration)='object'),
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now()+interval '90 days'),
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX customer_outcome_sources_scope_idx ON customer_outcome_sources(workspace_id,environment_id,source_kind);

CREATE TABLE customer_direct_outcome_observations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    source_id UUID NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('kubernetes_state','argocd','prometheus')),
    source_event_id TEXT NOT NULL,
    source_digest BYTEA NOT NULL CHECK (octet_length(source_digest)=32),
    source_raw BYTEA NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload)='object'),
    observed_at TIMESTAMPTZ NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    server_certificate_sha256 BYTEA NOT NULL CHECK (octet_length(server_certificate_sha256)=32),
    FOREIGN KEY (workspace_id,environment_id,source_id) REFERENCES customer_outcome_sources(workspace_id,environment_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    UNIQUE (source_id,source_event_id)
);
CREATE INDEX customer_direct_outcome_scope_idx ON customer_direct_outcome_observations(workspace_id,environment_id,session_id,source_kind,observed_at);
CREATE TRIGGER customer_direct_outcome_immutable BEFORE UPDATE OR DELETE ON customer_direct_outcome_observations FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();

-- Customer approval withdrawal and the connected gateway's actual pre-action
-- decisions are append-only. They do not change historical approval records.
CREATE TABLE operational_approval_revocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    approval_id UUID NOT NULL UNIQUE REFERENCES operational_approval_records(id) ON DELETE RESTRICT,
    revoked_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL DEFAULT '',
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE TRIGGER operational_approval_revocations_immutable BEFORE UPDATE OR DELETE ON operational_approval_revocations FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();

ALTER TABLE customer_execution_issuers ADD CONSTRAINT customer_execution_issuers_preflight_scope_key UNIQUE (workspace_id,environment_id,id,agent_version_id);
CREATE TABLE customer_gateway_preflight_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    issuer_id UUID NOT NULL,
    agent_version_id UUID NOT NULL,
    action_id UUID NOT NULL,
    operation TEXT NOT NULL,
    resource_namespace TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    image TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('allowed','denied')),
    reason_code TEXT NOT NULL,
    contract_references JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(contract_references)='array'),
    approval_references JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(approval_references)='array'),
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,issuer_id,agent_version_id) REFERENCES customer_execution_issuers(workspace_id,environment_id,id,agent_version_id) ON DELETE RESTRICT,
    UNIQUE (issuer_id,action_id)
);
CREATE INDEX customer_gateway_preflight_scope_idx ON customer_gateway_preflight_decisions(workspace_id,environment_id,session_id,decided_at);
CREATE TRIGGER customer_gateway_preflight_immutable BEFORE UPDATE OR DELETE ON customer_gateway_preflight_decisions FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
