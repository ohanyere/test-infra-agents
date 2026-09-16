DROP TRIGGER IF EXISTS agent_versions_immutable_identity ON agent_versions;
DROP FUNCTION IF EXISTS prevent_agent_version_identity_update();
DROP TABLE IF EXISTS agent_version_connections;
ALTER TABLE agent_versions DROP COLUMN IF EXISTS endpoint_url;
