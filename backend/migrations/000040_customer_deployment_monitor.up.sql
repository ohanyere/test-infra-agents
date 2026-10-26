-- Audit-triggered Deployment monitoring extends the existing completed-session
-- monitor. Periodic plans keep their original behavior.
ALTER TABLE assurance_monitoring_plans ADD COLUMN mode TEXT NOT NULL DEFAULT 'periodic'
    CHECK (mode IN ('periodic','deployment_audit'));
ALTER TABLE assurance_monitoring_plans DROP CONSTRAINT assurance_monitoring_plans_state_check;
ALTER TABLE assurance_monitoring_plans ADD CONSTRAINT assurance_monitoring_plans_state_check
    CHECK (state IN ('initializing','active','running','degraded','suspended','completed','failed','cancelled'));
ALTER TABLE assurance_monitoring_plans ADD COLUMN baseline_observation_id UUID REFERENCES customer_direct_outcome_observations(id) ON DELETE RESTRICT;
ALTER TABLE assurance_monitoring_plans ADD COLUMN assessment_baseline_observation_id UUID REFERENCES customer_direct_outcome_observations(id) ON DELETE RESTRICT;
ALTER TABLE assurance_monitoring_plans ADD COLUMN baseline_acquired_at TIMESTAMPTZ;
ALTER TABLE assurance_monitoring_plans ADD COLUMN last_audit_at TIMESTAMPTZ;
ALTER TABLE assurance_monitoring_plans ADD COLUMN integrity_gap BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE assurance_monitoring_plans ADD COLUMN deployment_source_id UUID REFERENCES customer_outcome_sources(id) ON DELETE RESTRICT;
ALTER TABLE assurance_monitoring_plans ADD CONSTRAINT deployment_monitor_baseline_pair
    CHECK ((baseline_observation_id IS NULL) = (baseline_acquired_at IS NULL));
ALTER TABLE assurance_monitoring_plans ADD CONSTRAINT deployment_monitor_source_required
    CHECK (mode <> 'deployment_audit' OR deployment_source_id IS NOT NULL);
CREATE UNIQUE INDEX deployment_monitor_one_live_target ON assurance_monitoring_plans(deployment_source_id)
    WHERE mode='deployment_audit' AND state IN ('initializing','active','running','degraded','suspended');

-- Authenticated events that cannot be assigned a pre-action baseline remain
-- inspectable as monitoring gaps, rather than silently disappearing.
CREATE TABLE customer_monitoring_audit_gaps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL REFERENCES assurance_monitoring_plans(session_id) ON DELETE RESTRICT,
    source_id UUID NOT NULL REFERENCES customer_audit_mtls_sources(id) ON DELETE RESTRICT,
    source_event_id TEXT NOT NULL,
    source_event_digest BYTEA NOT NULL CHECK (octet_length(source_event_digest)=32),
    event_raw BYTEA NOT NULL,
    event_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id,environment_id,source_event_id)
);
CREATE TRIGGER customer_monitoring_audit_gaps_immutable BEFORE UPDATE OR DELETE ON customer_monitoring_audit_gaps
    FOR EACH ROW EXECUTE FUNCTION prevent_customer_trust_history_change();
