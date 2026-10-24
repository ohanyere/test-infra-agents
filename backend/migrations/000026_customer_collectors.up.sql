-- Customer-owned Kubernetes environments are references observed by a
-- customer-hosted, read-only collector. No kubeconfig or cluster credential is
-- stored by Realverax.
CREATE TABLE customer_kubernetes_environments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    cluster_identifier TEXT NOT NULL CHECK (char_length(cluster_identifier) BETWEEN 1 AND 200),
    namespaces TEXT[] NOT NULL CHECK (cardinality(namespaces) BETWEEN 1 AND 100),
    resource_kinds TEXT[] NOT NULL DEFAULT ARRAY['Deployment','Pod']::TEXT[]
        CHECK (cardinality(resource_kinds) BETWEEN 1 AND 2),
    environment_tier TEXT NOT NULL CHECK (environment_tier IN ('staging','production')),
    connection_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (connection_status IN ('pending','healthy','degraded','revoked')),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    CHECK (revoked_at IS NULL OR connection_status = 'revoked')
);
CREATE INDEX customer_kubernetes_environments_workspace_idx
    ON customer_kubernetes_environments(workspace_id, created_at DESC);

-- A single active credential per environment. Only its SHA-256 digest is
-- stored. Rotation replaces the digest atomically and invalidates the prior
-- credential.
CREATE TABLE customer_kubernetes_collectors (
    environment_id UUID PRIMARY KEY REFERENCES customer_kubernetes_environments(id) ON DELETE CASCADE,
    credential_hash BYTEA NOT NULL CHECK (octet_length(credential_hash) = 32),
    credential_version BIGINT NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE customer_observation_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','completed','revoked')),
    observation_window_seconds INTEGER NOT NULL CHECK (observation_window_seconds > 0),
    next_sequence BIGINT NOT NULL DEFAULT 1 CHECK (next_sequence > 0),
    last_captured_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    FOREIGN KEY (workspace_id, environment_id)
        REFERENCES customer_kubernetes_environments(workspace_id, id) ON DELETE CASCADE,
    UNIQUE (run_id),
    UNIQUE (id, environment_id)
);

CREATE TABLE customer_observation_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    batch_id UUID NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    schema_version TEXT NOT NULL CHECK (schema_version = 'customer-kubernetes-observation:v1'),
    phase TEXT NOT NULL CHECK (phase IN ('baseline','stability','final')),
    source_identity TEXT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    FOREIGN KEY (session_id, environment_id)
        REFERENCES customer_observation_sessions(id, environment_id) ON DELETE CASCADE,
    UNIQUE (session_id, sequence),
    UNIQUE (batch_id)
);
CREATE INDEX customer_observation_batches_session_time_idx
    ON customer_observation_batches(session_id, sequence);
