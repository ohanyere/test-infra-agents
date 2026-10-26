DROP TABLE customer_monitoring_audit_gaps;
DROP INDEX deployment_monitor_one_live_target;
ALTER TABLE assurance_monitoring_plans DROP CONSTRAINT deployment_monitor_source_required;
ALTER TABLE assurance_monitoring_plans DROP CONSTRAINT deployment_monitor_baseline_pair;
ALTER TABLE assurance_monitoring_plans DROP COLUMN deployment_source_id;
ALTER TABLE assurance_monitoring_plans DROP COLUMN integrity_gap;
ALTER TABLE assurance_monitoring_plans DROP COLUMN last_audit_at;
ALTER TABLE assurance_monitoring_plans DROP COLUMN baseline_acquired_at;
ALTER TABLE assurance_monitoring_plans DROP COLUMN assessment_baseline_observation_id;
ALTER TABLE assurance_monitoring_plans DROP COLUMN baseline_observation_id;
UPDATE assurance_monitoring_plans SET state='cancelled' WHERE state IN ('initializing','degraded');
ALTER TABLE assurance_monitoring_plans DROP CONSTRAINT assurance_monitoring_plans_state_check;
ALTER TABLE assurance_monitoring_plans ADD CONSTRAINT assurance_monitoring_plans_state_check
    CHECK (state IN ('active','running','suspended','completed','failed','cancelled'));
ALTER TABLE assurance_monitoring_plans DROP COLUMN mode;
