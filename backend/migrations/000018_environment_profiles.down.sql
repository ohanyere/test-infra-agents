ALTER TABLE scenario_certifications
    DROP COLUMN IF EXISTS environment_component_contract_version,
    DROP COLUMN IF EXISTS environment_profile_contract_version,
    DROP COLUMN IF EXISTS isolation_tier,
    DROP COLUMN IF EXISTS provider_contract_version,
    DROP COLUMN IF EXISTS provider_id;
DROP TABLE IF EXISTS environment_instances;
DROP TABLE IF EXISTS scenario_version_environment_requirements;
