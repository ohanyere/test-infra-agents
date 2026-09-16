DROP TABLE IF EXISTS suite_execution_run_variants;
DROP FUNCTION IF EXISTS reject_suite_run_variant_mutation();
ALTER TABLE evaluation_suite_version_scenarios DROP COLUMN IF EXISTS hidden_variants;
