-- Stage 39A: immutable WorldSpec and CompositionPlan contracts. This is
-- additive; existing ScenarioVersions, runs and environment instances remain
-- valid without a binding or child components.
CREATE TABLE world_specs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    scenario_version_id UUID NOT NULL UNIQUE REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    contract_version TEXT NOT NULL CHECK (contract_version = 'world-spec:v1'),
    compiler_id TEXT NOT NULL CHECK (char_length(compiler_id) BETWEEN 1 AND 200),
    normalized_spec JSONB NOT NULL CHECK (jsonb_typeof(normalized_spec) = 'object'),
    spec_hash TEXT NOT NULL CHECK (spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX world_specs_hash_idx ON world_specs(spec_hash);

CREATE TABLE composition_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    world_spec_id UUID NOT NULL REFERENCES world_specs(id) ON DELETE RESTRICT,
    world_spec_hash TEXT NOT NULL CHECK (world_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    contract_version TEXT NOT NULL CHECK (contract_version = 'composition-plan:v1'),
    normalized_plan JSONB NOT NULL CHECK (jsonb_typeof(normalized_plan) = 'object'),
    plan_hash TEXT NOT NULL UNIQUE CHECK (plan_hash ~ '^sha256:[0-9a-f]{64}$'),
    profile_id TEXT NOT NULL CHECK (char_length(profile_id) BETWEEN 1 AND 200),
    isolation_tier TEXT NOT NULL CHECK (char_length(isolation_tier) BETWEEN 1 AND 100),
    minimum_fidelity TEXT NOT NULL CHECK (minimum_fidelity IN ('legacy_unspecified','synthetic_fixture','local_emulated','isolated_replica','isolated_real_provider')),
    selected_fidelity TEXT NOT NULL CHECK (selected_fidelity IN ('legacy_unspecified','synthetic_fixture','local_emulated','isolated_replica','isolated_real_provider')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, scenario_version_id, plan_hash)
);
CREATE INDEX composition_plans_workspace_idx ON composition_plans(workspace_id, created_at DESC, id DESC);

ALTER TABLE environment_instances
    ADD COLUMN composition_plan_id UUID REFERENCES composition_plans(id) ON DELETE RESTRICT,
    ADD COLUMN selected_fidelity TEXT CHECK (selected_fidelity IS NULL OR selected_fidelity IN ('legacy_unspecified','synthetic_fixture','local_emulated','isolated_replica','isolated_real_provider'));

CREATE TABLE environment_component_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    environment_instance_id UUID NOT NULL REFERENCES environment_instances(id) ON DELETE CASCADE,
    component_id TEXT NOT NULL CHECK (char_length(component_id) BETWEEN 1 AND 100),
    component_type TEXT NOT NULL CHECK (char_length(component_type) BETWEEN 1 AND 100),
    component_version TEXT NOT NULL DEFAULT '' CHECK (char_length(component_version) <= 500),
    adapter_id TEXT NOT NULL CHECK (char_length(adapter_id) BETWEEN 1 AND 200),
    adapter_contract_version TEXT NOT NULL CHECK (char_length(adapter_contract_version) BETWEEN 1 AND 200),
    provider_id TEXT NOT NULL CHECK (char_length(provider_id) BETWEEN 1 AND 200),
    dependency_order INTEGER NOT NULL CHECK (dependency_order >= 0),
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','provisioning','ready','destroying','destroyed','failed')),
    provider_ref TEXT NOT NULL DEFAULT '' CHECK (char_length(provider_ref) <= 2000),
    reproducibility_metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(reproducibility_metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ,
    destroyed_at TIMESTAMPTZ,
    UNIQUE (environment_instance_id, component_id)
);
CREATE INDEX environment_component_instances_owner_idx ON environment_component_instances(workspace_id, environment_instance_id, dependency_order);

ALTER TABLE runs
    ADD COLUMN composition_plan_id UUID REFERENCES composition_plans(id) ON DELETE RESTRICT,
    ADD COLUMN environment_instance_id UUID REFERENCES environment_instances(id) ON DELETE SET NULL;
ALTER TABLE mcp_run_grants
    ADD COLUMN environment_instance_id UUID REFERENCES environment_instances(id) ON DELETE SET NULL,
    ADD COLUMN composition_plan_id UUID REFERENCES composition_plans(id) ON DELETE SET NULL,
    ADD COLUMN composition_plan_hash TEXT CHECK (composition_plan_hash IS NULL OR composition_plan_hash ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN component_scope TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN tool_contract_version TEXT;

CREATE FUNCTION prevent_world_spec_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'world specs are immutable'; END; $$;
CREATE TRIGGER world_specs_immutable BEFORE UPDATE ON world_specs FOR EACH ROW EXECUTE FUNCTION prevent_world_spec_update();
CREATE FUNCTION prevent_composition_plan_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'composition plans are immutable'; END; $$;
CREATE TRIGGER composition_plans_immutable BEFORE UPDATE ON composition_plans FOR EACH ROW EXECUTE FUNCTION prevent_composition_plan_update();

CREATE FUNCTION enforce_environment_plan_immutability() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.composition_plan_id IS NOT NULL AND NEW.composition_plan_id IS DISTINCT FROM OLD.composition_plan_id THEN
        RAISE EXCEPTION 'environment composition plan binding is immutable';
    END IF;
    IF OLD.selected_fidelity IS NOT NULL AND NEW.selected_fidelity IS DISTINCT FROM OLD.selected_fidelity THEN
        RAISE EXCEPTION 'environment fidelity binding is immutable';
    END IF;
    RETURN NEW;
END; $$;
CREATE TRIGGER environment_plan_immutable BEFORE UPDATE ON environment_instances FOR EACH ROW EXECUTE FUNCTION enforce_environment_plan_immutability();
