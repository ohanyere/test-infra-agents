-- External-agent action claims are customer-submitted supporting evidence.
-- They are intentionally not inserted into agent_actions or run_events.
ALTER TABLE customer_observation_sessions
    ADD CONSTRAINT customer_observation_sessions_workspace_environment_id_key
    UNIQUE (workspace_id, environment_id, id);

CREATE TABLE customer_agent_action_claims (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_version TEXT NOT NULL CHECK (contract_version = 'customer-agent-action-claim:v1'),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    claim_id UUID NOT NULL,
    idempotency_key UUID NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN (
        'set_workload_image', 'set_live_workload_image',
        'set_desired_workload_image', 'scale_workload', 'restart_pod'
    )),
    resource_kind TEXT NOT NULL CHECK (resource_kind IN ('Deployment','Pod')),
    resource_namespace TEXT NOT NULL CHECK (char_length(resource_namespace) BETWEEN 1 AND 63),
    resource_name TEXT NOT NULL CHECK (char_length(resource_name) BETWEEN 1 AND 253),
    resource_uid TEXT NOT NULL CHECK (char_length(resource_uid) BETWEEN 1 AND 128),
    claimed_started_at TIMESTAMPTZ NOT NULL,
    claimed_completed_at TIMESTAMPTZ NOT NULL,
    claimed_outcome TEXT NOT NULL CHECK (claimed_outcome IN ('proposed','authorized','denied','executed','failed')),
    external_trace_reference TEXT CHECK (external_trace_reference IS NULL OR external_trace_reference ~ '^[A-Fa-f0-9]{16,64}$'),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitter_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    trust_classification TEXT NOT NULL DEFAULT 'UNVERIFIED_AGENT_CLAIM'
        CHECK (trust_classification = 'UNVERIFIED_AGENT_CLAIM'),
    FOREIGN KEY (workspace_id, environment_id)
        REFERENCES customer_kubernetes_environments(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, environment_id, session_id)
        REFERENCES customer_observation_sessions(workspace_id, environment_id, id) ON DELETE CASCADE,
    CHECK (claimed_completed_at >= claimed_started_at),
    UNIQUE (session_id, claim_id),
    UNIQUE (session_id, idempotency_key)
);

CREATE INDEX customer_agent_action_claims_session_submitted_idx
    ON customer_agent_action_claims(workspace_id, environment_id, session_id, submitted_at, id);
