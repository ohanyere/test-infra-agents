DROP TRIGGER IF EXISTS verification_results_immutable ON verification_results;
DROP FUNCTION IF EXISTS prevent_verification_result_mutation();
DROP TABLE IF EXISTS verification_results;
DROP INDEX IF EXISTS evaluation_batches_ci_idempotency_idx;
ALTER TABLE evaluation_batches DROP COLUMN IF EXISTS idempotency_key, DROP COLUMN IF EXISTS source_metadata;
