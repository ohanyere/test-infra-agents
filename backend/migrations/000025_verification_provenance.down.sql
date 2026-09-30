ALTER TABLE verification_results DROP COLUMN provenance;

DROP TRIGGER runs_immutable_provenance ON runs;
DROP FUNCTION prevent_run_provenance_update();
ALTER TABLE runs DROP COLUMN agent_implementation_identity;

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

ALTER TABLE agent_versions DROP COLUMN implementation_identity;
