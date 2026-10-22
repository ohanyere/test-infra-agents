ALTER TABLE assurance_notification_events DROP CONSTRAINT assurance_notification_events_event_kind_check;
ALTER TABLE assurance_notification_events ADD CONSTRAINT assurance_notification_events_event_kind_check CHECK (event_kind IN ('confirmed_violation','candidate_violation','evidence_unavailable','stability_regression','reconciliation_failure'));
DROP TRIGGER IF EXISTS assurance_monitoring_events_immutable ON assurance_monitoring_events;
DROP TABLE IF EXISTS assurance_monitoring_events;
DROP INDEX IF EXISTS customer_direct_outcome_cycle_source_key;
ALTER TABLE customer_direct_outcome_observations DROP COLUMN IF EXISTS monitoring_cycle_id;
DROP TABLE IF EXISTS assurance_monitoring_plan_sources;
DROP TABLE IF EXISTS assurance_monitoring_plans;
