DROP TABLE IF EXISTS agent_regression_comparisons;
DROP FUNCTION IF EXISTS reject_agent_regression_mutation();
ALTER TABLE evaluations DROP COLUMN IF EXISTS evaluator_version;
