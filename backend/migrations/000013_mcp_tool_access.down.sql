DROP TRIGGER IF EXISTS runs_revoke_mcp_grants ON runs;
DROP FUNCTION IF EXISTS revoke_mcp_grants_for_terminal_run();
DROP TABLE IF EXISTS mcp_run_grants;

CREATE OR REPLACE FUNCTION prevent_agent_version_identity_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.version_label IS DISTINCT FROM OLD.version_label
       OR NEW.interface_kind IS DISTINCT FROM OLD.interface_kind
       OR NEW.capabilities IS DISTINCT FROM OLD.capabilities
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.endpoint_url IS DISTINCT FROM OLD.endpoint_url THEN
        RAISE EXCEPTION 'agent version identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE agent_versions DROP COLUMN IF EXISTS tool_access_mode;
