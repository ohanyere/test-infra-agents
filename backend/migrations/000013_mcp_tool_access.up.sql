ALTER TABLE agent_versions
    ADD COLUMN tool_access_mode TEXT NOT NULL DEFAULT 'direct'
        CHECK (tool_access_mode IN ('direct', 'mcp'));

CREATE TABLE mcp_run_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    tool_calls INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
    max_tool_calls INTEGER NOT NULL CHECK (max_tool_calls BETWEEN 1 AND 1000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, agent_version_id)
);

CREATE INDEX mcp_run_grants_active_idx
    ON mcp_run_grants (token_hash, expires_at)
    WHERE revoked_at IS NULL;

CREATE FUNCTION revoke_mcp_grants_for_terminal_run()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('completed', 'failed', 'stopped') AND NEW.status IS DISTINCT FROM OLD.status THEN
        UPDATE mcp_run_grants SET revoked_at=COALESCE(revoked_at,now()) WHERE run_id=NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runs_revoke_mcp_grants
AFTER UPDATE OF status ON runs
FOR EACH ROW EXECUTE FUNCTION revoke_mcp_grants_for_terminal_run();

CREATE OR REPLACE FUNCTION prevent_agent_version_identity_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.version_label IS DISTINCT FROM OLD.version_label
       OR NEW.interface_kind IS DISTINCT FROM OLD.interface_kind
       OR NEW.capabilities IS DISTINCT FROM OLD.capabilities
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.endpoint_url IS DISTINCT FROM OLD.endpoint_url
       OR NEW.tool_access_mode IS DISTINCT FROM OLD.tool_access_mode THEN
        RAISE EXCEPTION 'agent version identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
