DROP TABLE customer_action_evidence;
DROP FUNCTION prevent_customer_action_evidence_truncate();
DROP FUNCTION prevent_customer_action_evidence_update();
ALTER TABLE customer_observation_sessions
    DROP CONSTRAINT customer_observation_sessions_scope_run_key;
