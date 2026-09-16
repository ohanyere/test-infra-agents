ALTER TABLE agent_versions
    ADD COLUMN endpoint_url TEXT;

CREATE TABLE agent_version_connections (
    agent_version_id UUID PRIMARY KEY REFERENCES agent_versions(id) ON DELETE CASCADE,
    auth_type TEXT NOT NULL DEFAULT 'none' CHECK (auth_type IN ('none', 'bearer')),
    credential_envelope BYTEA,
    credential_updated_at TIMESTAMPTZ,
    last_checked_at TIMESTAMPTZ,
    last_check_status TEXT NOT NULL DEFAULT 'untested'
        CHECK (last_check_status IN ('untested', 'healthy', 'unreachable', 'authentication_failed', 'protocol_error', 'timeout', 'configuration_error')),
    last_diagnostic_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((auth_type = 'none' AND credential_envelope IS NULL) OR
           (auth_type = 'bearer' AND credential_envelope IS NOT NULL))
);

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

CREATE TRIGGER agent_versions_immutable_identity
BEFORE UPDATE ON agent_versions
FOR EACH ROW EXECUTE FUNCTION prevent_agent_version_identity_update();
