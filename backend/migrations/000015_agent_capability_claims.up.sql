CREATE TABLE agent_version_capability_claims (
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE CASCADE,
    capability_id TEXT NOT NULL CHECK (char_length(capability_id) BETWEEN 1 AND 200),
    catalog_version TEXT NOT NULL DEFAULT 'infrastructure-capability-catalog:v1'
        CHECK (catalog_version = 'infrastructure-capability-catalog:v1'),
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_version_id, capability_id)
);

CREATE INDEX agent_version_capability_claims_capability_idx
    ON agent_version_capability_claims (capability_id, agent_version_id);

CREATE FUNCTION reject_capability_claim_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'agent version capability claims are immutable: register a new AgentVersion' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER agent_version_capability_claims_immutable
BEFORE UPDATE OR DELETE ON agent_version_capability_claims
FOR EACH ROW EXECUTE FUNCTION reject_capability_claim_mutation();
