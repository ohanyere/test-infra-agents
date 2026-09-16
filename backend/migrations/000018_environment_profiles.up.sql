-- Environment profiles & composition (V1).
--
-- Composable model: components -> profiles -> providers -> scheduler ->
-- provisioned EnvironmentInstance. This migration is the persistence layer
-- only; catalogs themselves are server-owned Go constants pinned by contract
-- versions (environment-component-catalog:v1,
-- environment-profile-catalog:v1, environment-provider-contract:v1,
-- environment-scheduler-contract:v1).
--
-- No AWS/GCP/Terraform implementation. No cloud credentials are stored:
-- environment_instances.provider_ref is a safe opaque handle only (never a
-- kubeconfig, token, or secret).

-- Server-reviewed environment requirements for one exact ScenarioVersion.
-- Evaluator metadata only: no ground truth, reference remediation, hidden
-- variant values, or grader internals.
CREATE TABLE scenario_version_environment_requirements (
    scenario_version_id UUID PRIMARY KEY REFERENCES scenario_versions(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    required_components TEXT[] NOT NULL DEFAULT '{kubernetes,prometheus}'
        CHECK (array_length(required_components, 1) BETWEEN 1 AND 20),
    minimum_isolation_tier TEXT NOT NULL DEFAULT 'namespace'
        CHECK (minimum_isolation_tier IN ('namespace', 'virtual_cluster', 'dedicated_cluster')),
    required_profile_capabilities TEXT[] NOT NULL DEFAULT '{}',
    runtime_contract_version TEXT NOT NULL DEFAULT 'kubernetes-traffic-spike:v1'
        CHECK (char_length(runtime_contract_version) BETWEEN 1 AND 200),
    contract_version TEXT NOT NULL DEFAULT 'environment-requirements:v1'
        CHECK (contract_version = 'environment-requirements:v1'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Canonical provisioned environment. Every environment belongs to a
-- workspace and a specific execution owner (run or certification); instances
-- are never reused across unrelated tenants.
CREATE TABLE environment_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('run', 'certification')),
    owner_id UUID NOT NULL,
    profile_id TEXT NOT NULL CHECK (char_length(profile_id) BETWEEN 1 AND 200),
    profile_contract_version TEXT NOT NULL DEFAULT 'environment-profile-catalog:v1'
        CHECK (char_length(profile_contract_version) BETWEEN 1 AND 200),
    provider_id TEXT NOT NULL CHECK (char_length(provider_id) BETWEEN 1 AND 200),
    provider_contract_version TEXT NOT NULL DEFAULT 'environment-provider-contract:v1'
        CHECK (char_length(provider_contract_version) BETWEEN 1 AND 200),
    isolation_tier TEXT NOT NULL CHECK (isolation_tier IN ('namespace', 'virtual_cluster', 'dedicated_cluster')),
    status TEXT NOT NULL DEFAULT 'provisioning'
        CHECK (status IN ('provisioning', 'ready', 'resetting', 'destroying', 'destroyed', 'failed')),
    lease_expires_at TIMESTAMPTZ,
    provider_ref TEXT NOT NULL DEFAULT '' CHECK (char_length(provider_ref) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ,
    destroyed_at TIMESTAMPTZ
);

CREATE INDEX environment_instances_workspace_idx
    ON environment_instances (workspace_id, created_at DESC, id DESC);
CREATE INDEX environment_instances_owner_idx
    ON environment_instances (workspace_id, owner_type, owner_id);

-- Certification pins the exact environment/profile/provider/runtime
-- contracts evidence was produced under. Nullable so historical rows stay
-- immutable and interpretable; new attempts always pin all five.
ALTER TABLE scenario_certifications
    ADD COLUMN provider_id TEXT CHECK (provider_id IS NULL OR char_length(provider_id) BETWEEN 1 AND 200),
    ADD COLUMN provider_contract_version TEXT NOT NULL DEFAULT 'environment-provider-contract:v1'
        CHECK (char_length(provider_contract_version) BETWEEN 1 AND 200),
    ADD COLUMN isolation_tier TEXT CHECK (isolation_tier IS NULL OR isolation_tier IN ('namespace', 'virtual_cluster', 'dedicated_cluster')),
    ADD COLUMN environment_profile_contract_version TEXT NOT NULL DEFAULT 'environment-profile-catalog:v1'
        CHECK (char_length(environment_profile_contract_version) BETWEEN 1 AND 200),
    ADD COLUMN environment_component_contract_version TEXT NOT NULL DEFAULT 'environment-component-catalog:v1'
        CHECK (char_length(environment_component_contract_version) BETWEEN 1 AND 200);
