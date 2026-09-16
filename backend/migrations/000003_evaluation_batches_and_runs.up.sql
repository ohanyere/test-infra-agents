CREATE TABLE evaluation_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'queued', 'running', 'completed', 'failed', 'stopped')),
    runs_per_agent INTEGER NOT NULL DEFAULT 1 CHECK (runs_per_agent > 0 AND runs_per_agent <= 20),
    environment_mode TEXT NOT NULL DEFAULT 'isolated' CHECK (environment_mode = 'isolated'),
    config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX evaluation_batches_workspace_status_created_idx ON evaluation_batches (workspace_id, status, created_at DESC);
CREATE INDEX evaluation_batches_scenario_version_id_idx ON evaluation_batches (scenario_version_id);

CREATE TABLE evaluation_batch_agents (
    batch_id UUID NOT NULL REFERENCES evaluation_batches(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id),
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (batch_id, agent_version_id)
);

CREATE INDEX evaluation_batch_agents_agent_version_id_idx ON evaluation_batch_agents (agent_version_id);

CREATE TABLE runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES evaluation_batches(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id),
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'provisioning', 'running', 'completed', 'failed', 'stopped')),
    environment_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE (batch_id, agent_version_id, attempt_number),
    FOREIGN KEY (batch_id, agent_version_id) REFERENCES evaluation_batch_agents(batch_id, agent_version_id)
);

CREATE INDEX runs_batch_status_idx ON runs (batch_id, status);
CREATE INDEX runs_agent_version_id_idx ON runs (agent_version_id);
CREATE INDEX runs_status_created_at_idx ON runs (status, created_at DESC);

CREATE TABLE run_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('scenario', 'observation', 'action', 'decision', 'recovery', 'evaluation', 'safety', 'system')),
    occurred_at TIMESTAMPTZ NOT NULL,
    summary TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, sequence)
);

CREATE INDEX run_events_run_occurred_at_idx ON run_events (run_id, occurred_at);

CREATE TABLE agent_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    run_event_id BIGINT REFERENCES run_events(id) ON DELETE SET NULL,
    action_type TEXT NOT NULL,
    target_type TEXT,
    target_ref TEXT,
    status TEXT NOT NULL CHECK (status IN ('proposed', 'allowed', 'denied', 'executed', 'failed')),
    authorized BOOLEAN NOT NULL DEFAULT false,
    request_payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(request_payload) = 'object'),
    result_payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(result_payload) = 'object'),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON COLUMN agent_actions.request_payload IS 'Sensitive material must be redacted before persistence.';
COMMENT ON COLUMN agent_actions.result_payload IS 'Sensitive material must be redacted before persistence.';
CREATE INDEX agent_actions_run_id_idx ON agent_actions (run_id);
CREATE INDEX agent_actions_status_idx ON agent_actions (status);
CREATE INDEX agent_actions_authorized_idx ON agent_actions (authorized);
