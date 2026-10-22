-- Mutable scheduling state is separate from immutable source and assessment history.
CREATE TABLE assurance_monitoring_plans (
    session_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    run_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','running','suspended','completed','failed','cancelled')),
    cadence_seconds INTEGER NOT NULL CHECK (cadence_seconds BETWEEN 60 AND 3600),
    ends_at TIMESTAMPTZ NOT NULL,
    next_due_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    current_cycle_id UUID,
    cycle_number INTEGER NOT NULL DEFAULT 0 CHECK (cycle_number BETWEEN 0 AND 288),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    lease_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_success_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id)
        REFERENCES customer_observation_sessions(workspace_id,environment_id,id,run_id) ON DELETE RESTRICT
);
CREATE INDEX assurance_monitoring_due_idx ON assurance_monitoring_plans(state,next_due_at,lease_until);
CREATE TABLE assurance_monitoring_plan_sources (
    session_id UUID NOT NULL REFERENCES assurance_monitoring_plans(session_id) ON DELETE RESTRICT,
    source_id UUID NOT NULL REFERENCES customer_outcome_sources(id) ON DELETE RESTRICT,
    PRIMARY KEY (session_id,source_id)
);
ALTER TABLE customer_direct_outcome_observations ADD COLUMN monitoring_cycle_id UUID;
CREATE UNIQUE INDEX customer_direct_outcome_cycle_source_key ON customer_direct_outcome_observations(source_id,monitoring_cycle_id) WHERE monitoring_cycle_id IS NOT NULL;
CREATE TABLE assurance_monitoring_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    cycle_id UUID NOT NULL,
    cycle_number INTEGER NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 5),
    source_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('acquired','unavailable')),
    observation_id UUID REFERENCES customer_direct_outcome_observations(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL DEFAULT '',
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,session_id) REFERENCES customer_observation_sessions(workspace_id,environment_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (session_id,source_id) REFERENCES assurance_monitoring_plan_sources(session_id,source_id) ON DELETE RESTRICT,
    UNIQUE (cycle_id,source_id,attempt)
);
CREATE INDEX assurance_monitoring_events_scope_idx ON assurance_monitoring_events(workspace_id,environment_id,session_id,recorded_at);
CREATE TRIGGER assurance_monitoring_events_immutable BEFORE UPDATE OR DELETE ON assurance_monitoring_events FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
ALTER TABLE assurance_notification_events DROP CONSTRAINT assurance_notification_events_event_kind_check;
ALTER TABLE assurance_notification_events ADD CONSTRAINT assurance_notification_events_event_kind_check CHECK (event_kind IN ('confirmed_violation','candidate_violation','evidence_unavailable','stability_regression','reconciliation_failure','assurance_regression','assurance_recovery'));
