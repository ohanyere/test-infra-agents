ALTER TABLE customer_observation_sessions
    ADD CONSTRAINT customer_observation_sessions_scope_run_key
    UNIQUE (workspace_id, environment_id, id, run_id);

CREATE TABLE customer_action_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_version TEXT NOT NULL CHECK (contract_version = 'customer-independent-action-evidence:v1'),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('agent_declared_action','observed_state','authenticated_action_record')),
    source_id TEXT NOT NULL CHECK (char_length(source_id) BETWEEN 1 AND 128),
    source_identity TEXT NOT NULL DEFAULT '' CHECK (char_length(source_identity) <= 256),
    source_event_id TEXT NOT NULL CHECK (char_length(source_event_id) BETWEEN 1 AND 256),
    action_type TEXT CHECK (action_type IS NULL OR char_length(action_type) <= 100),
    operation TEXT CHECK (operation IS NULL OR char_length(operation) <= 100),
    target_kind TEXT CHECK (target_kind IS NULL OR char_length(target_kind) <= 100),
    target_namespace TEXT CHECK (target_namespace IS NULL OR char_length(target_namespace) <= 63),
    target_name TEXT CHECK (target_name IS NULL OR char_length(target_name) <= 253),
    target_uid TEXT CHECK (target_uid IS NULL OR char_length(target_uid) <= 128),
    actor_identity TEXT CHECK (actor_identity IS NULL OR char_length(actor_identity) <= 512),
    actor_authority TEXT CHECK (actor_authority IS NULL OR char_length(actor_authority) <= 256),
    actor_identity_status TEXT NOT NULL CHECK (actor_identity_status IN ('verified','asserted','unknown','conflicting')),
    occurred_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    authentication_method TEXT NOT NULL CHECK (char_length(authentication_method) BETWEEN 1 AND 128),
    credential_scope TEXT CHECK (credential_scope IS NULL OR char_length(credential_scope) <= 256),
    authentication_status TEXT NOT NULL CHECK (authentication_status IN ('verified','unverified','failed','unknown')),
    authorization_status TEXT NOT NULL CHECK (authorization_status IN ('authorized','denied','unknown')),
    provenance_verification_status TEXT NOT NULL CHECK (provenance_verification_status IN ('verified','insufficient','conflicting','failed','unknown')),
    causality_status TEXT NOT NULL CHECK (causality_status IN ('qualified','unproven','conflicting','not_applicable')),
    trust_classification TEXT NOT NULL CHECK (trust_classification IN (
        'UNVERIFIED_AGENT_CLAIM','INDEPENDENTLY_OBSERVED_STATE','AUTHENTICATED_ACTION_RECORD',
        'INSUFFICIENT_PROVENANCE','CONFLICTING_PROVENANCE','ATTRIBUTION_QUALIFIED'
    )),
    correlation_ids JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(correlation_ids) = 'array'),
    evidence_references JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_references) = 'array'),
    provenance_conflicts JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(provenance_conflicts) = 'array'),
    uncertainty JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(uncertainty) = 'array'),
    missing_fields JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(missing_fields) = 'array'),
    payload_digest BYTEA NOT NULL CHECK (octet_length(payload_digest) = 32),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    FOREIGN KEY (workspace_id, environment_id, session_id, run_id)
        REFERENCES customer_observation_sessions(workspace_id, environment_id, id, run_id) ON DELETE RESTRICT,
    UNIQUE (session_id, source_id, source_identity, source_event_id)
);

CREATE INDEX customer_action_evidence_workspace_run_time_idx
    ON customer_action_evidence(workspace_id, run_id, occurred_at, id);

CREATE FUNCTION prevent_customer_action_evidence_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'customer action evidence is immutable';
END;
$$;

CREATE TRIGGER customer_action_evidence_immutable
BEFORE UPDATE OR DELETE ON customer_action_evidence
FOR EACH ROW EXECUTE FUNCTION prevent_customer_action_evidence_update();

CREATE FUNCTION prevent_customer_action_evidence_truncate()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM customer_action_evidence) THEN
        RAISE EXCEPTION 'customer action evidence is immutable';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER customer_action_evidence_no_truncate
BEFORE TRUNCATE ON customer_action_evidence
FOR EACH STATEMENT EXECUTE FUNCTION prevent_customer_action_evidence_truncate();

COMMENT ON TABLE customer_action_evidence IS
    'Append-only provider evidence. Source authentication and trust are server-assessed; SHA-256 payload digest detects content changes but does not authenticate the source.';
