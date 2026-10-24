ALTER TABLE agent_versions
    ADD COLUMN implementation_identity JSONB NOT NULL
        DEFAULT '{"contract_version":"agent-implementation-identity:v1","kind":"legacy","identifier":"unavailable","provenance_level":"legacy"}'::jsonb
        CHECK (jsonb_typeof(implementation_identity) = 'object'
            AND implementation_identity->>'contract_version' = 'agent-implementation-identity:v1'
            AND implementation_identity->>'kind' IN ('artifact_digest','source_revision','deployment_revision','model_provider','declared','legacy')
            AND implementation_identity->>'identifier' <> ''
            AND implementation_identity->>'provenance_level' IN ('declared','legacy')
            AND ((implementation_identity->>'provenance_level' = 'legacy' AND implementation_identity->>'kind' = 'legacy')
                OR (implementation_identity->>'provenance_level' = 'declared' AND implementation_identity->>'kind' <> 'legacy')));

CREATE OR REPLACE FUNCTION prevent_agent_version_identity_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.version_label IS DISTINCT FROM OLD.version_label
       OR NEW.interface_kind IS DISTINCT FROM OLD.interface_kind
       OR NEW.capabilities IS DISTINCT FROM OLD.capabilities
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.endpoint_url IS DISTINCT FROM OLD.endpoint_url
       OR NEW.tool_access_mode IS DISTINCT FROM OLD.tool_access_mode
       OR NEW.implementation_identity IS DISTINCT FROM OLD.implementation_identity THEN
        RAISE EXCEPTION 'agent version identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE runs
    ADD COLUMN agent_implementation_identity JSONB NOT NULL
        DEFAULT '{"contract_version":"agent-implementation-identity:v1","kind":"legacy","identifier":"unavailable","provenance_level":"legacy"}'::jsonb
        CHECK (jsonb_typeof(agent_implementation_identity) = 'object'
            AND agent_implementation_identity->>'contract_version' = 'agent-implementation-identity:v1'
            AND agent_implementation_identity->>'kind' IN ('artifact_digest','source_revision','deployment_revision','model_provider','declared','legacy')
            AND agent_implementation_identity->>'identifier' <> ''
            AND agent_implementation_identity->>'provenance_level' IN ('declared','legacy')
            AND ((agent_implementation_identity->>'provenance_level' = 'legacy' AND agent_implementation_identity->>'kind' = 'legacy')
                OR (agent_implementation_identity->>'provenance_level' = 'declared' AND agent_implementation_identity->>'kind' <> 'legacy')));

UPDATE runs r
SET agent_implementation_identity = av.implementation_identity
FROM agent_versions av
WHERE av.id = r.agent_version_id;

CREATE FUNCTION prevent_run_provenance_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_implementation_identity IS DISTINCT FROM OLD.agent_implementation_identity THEN
        RAISE EXCEPTION 'run provenance is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runs_immutable_provenance
BEFORE UPDATE ON runs
FOR EACH ROW EXECUTE FUNCTION prevent_run_provenance_update();

ALTER TABLE verification_results
    ADD COLUMN provenance JSONB
        CHECK (provenance IS NULL OR (jsonb_typeof(provenance) = 'object'
            AND provenance->>'contract_version' = 'verification-provenance:v1'
            AND jsonb_typeof(provenance->'agent_implementation') = 'object'
            AND provenance->'agent_implementation'->>'contract_version' = 'agent-implementation-identity:v1'
            AND provenance->'agent_implementation'->>'identifier' <> ''
            AND provenance->'agent_implementation'->>'provenance_level' IN ('declared','legacy')
            AND jsonb_typeof(provenance->'contracts') = 'object'
            AND jsonb_typeof(provenance->'providers') = 'array'));

COMMENT ON COLUMN agent_versions.implementation_identity IS
    'Immutable, typed implementation identity. declared is pinned but not cryptographically attested; legacy denotes unavailable historical identity.';
COMMENT ON COLUMN runs.agent_implementation_identity IS
    'Immutable snapshot of the selected AgentVersion implementation identity at run creation.';
COMMENT ON COLUMN verification_results.provenance IS
    'Immutable Stage 4 provenance snapshot. NULL only for historical pre-Stage-4 results.';
