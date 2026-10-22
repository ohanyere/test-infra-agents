-- Materialized experience retains the complete immutable source report.
-- The existing report identity is unique by workspace, run, and revision.
-- This additional key lets episode foreign keys enforce the exact registered
-- environment and session as well.
ALTER TABLE operational_assessment_reports
    ADD CONSTRAINT operational_assessment_reports_episode_scope_key
    UNIQUE (workspace_id,environment_id,session_id,run_id,revision);

CREATE TABLE operational_episodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    report_revision INTEGER NOT NULL,
    content_digest TEXT NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    eligibility TEXT NOT NULL CHECK (eligibility IN ('verified','provisional','inconclusive','conflicting')),
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_namespace TEXT NOT NULL DEFAULT '',
    resource_name TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL DEFAULT '',
    episode JSONB NOT NULL CHECK (jsonb_typeof(episode) = 'object'),
    payload_bytes BYTEA NOT NULL CHECK (octet_length(payload_bytes) > 0),
    evaluated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT operational_episode_report_scope_fkey
        FOREIGN KEY (workspace_id,environment_id,session_id,run_id,report_revision)
        REFERENCES operational_assessment_reports(workspace_id,environment_id,session_id,run_id,revision) ON DELETE RESTRICT,
    UNIQUE (workspace_id,run_id,report_revision),
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX operational_episodes_lookup ON operational_episodes(workspace_id,environment_id,eligibility,resource_kind,resource_namespace,service_name,evaluated_at DESC);
CREATE TRIGGER operational_episodes_immutable BEFORE UPDATE OR DELETE ON operational_episodes FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_episodes_no_truncate BEFORE TRUNCATE ON operational_episodes FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_episode_relationships (
    episode_id UUID NOT NULL REFERENCES operational_episodes(id) ON DELETE RESTRICT,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    from_kind TEXT NOT NULL,
    from_ref TEXT NOT NULL,
    relation TEXT NOT NULL,
    to_kind TEXT NOT NULL,
    to_ref TEXT NOT NULL,
    observed BOOLEAN NOT NULL,
    provenance TEXT NOT NULL,
    source_reference TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (episode_id,ordinal),
    FOREIGN KEY (workspace_id,environment_id,episode_id) REFERENCES operational_episodes(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE INDEX operational_episode_relationships_scope ON operational_episode_relationships(workspace_id,environment_id,from_kind,from_ref);
CREATE TRIGGER operational_episode_relationships_immutable BEFORE UPDATE OR DELETE ON operational_episode_relationships FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_episode_relationships_no_truncate BEFORE TRUNCATE ON operational_episode_relationships FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_episode_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    episode_id UUID NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('revoked','restored')),
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,episode_id) REFERENCES operational_episodes(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE INDEX operational_episode_decisions_latest ON operational_episode_decisions(episode_id,created_at DESC,id DESC);
CREATE TRIGGER operational_episode_decisions_immutable BEFORE UPDATE OR DELETE ON operational_episode_decisions FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_episode_decisions_no_truncate BEFORE TRUNCATE ON operational_episode_decisions FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_lessons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    episode_id UUID NOT NULL,
    lesson_kind TEXT NOT NULL CHECK (lesson_kind IN ('observed_failure','observed_recovery','stability_window')),
    condition JSONB NOT NULL CHECK (jsonb_typeof(condition) = 'object'),
    outcome JSONB NOT NULL CHECK (jsonb_typeof(outcome) = 'object'),
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'array'),
    limitations TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,episode_id) REFERENCES operational_episodes(workspace_id,environment_id,id) ON DELETE RESTRICT,
    UNIQUE (episode_id,lesson_kind,condition),
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX operational_lessons_scope ON operational_lessons(workspace_id,environment_id,created_at DESC);
CREATE TRIGGER operational_lessons_immutable BEFORE UPDATE OR DELETE ON operational_lessons FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_lessons_no_truncate BEFORE TRUNCATE ON operational_lessons FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_lesson_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id UUID NOT NULL REFERENCES operational_lessons(id) ON DELETE RESTRICT,
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved','revoked')),
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,lesson_id) REFERENCES operational_lessons(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE INDEX operational_lesson_decisions_latest ON operational_lesson_decisions(lesson_id,created_at DESC,id DESC);
CREATE TRIGGER operational_lesson_decisions_immutable BEFORE UPDATE OR DELETE ON operational_lesson_decisions FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_lesson_decisions_no_truncate BEFORE TRUNCATE ON operational_lesson_decisions FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

-- A guidance credential authenticates a registered agent version for read-only
-- guidance. It does not authenticate Kubernetes operations or grant approval.
CREATE TABLE operational_guidance_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    namespaces TEXT[] NOT NULL,
    resource_kinds TEXT[] NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    issued_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX operational_guidance_credentials_scope ON operational_guidance_credentials(workspace_id,environment_id,agent_version_id);

CREATE TABLE operational_guidance_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    credential_id UUID,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    request JSONB NOT NULL CHECK (jsonb_typeof(request) = 'object'),
    response JSONB NOT NULL CHECK (jsonb_typeof(response) = 'object'),
    response_bytes BYTEA NOT NULL CHECK (octet_length(response_bytes) > 0),
    response_digest TEXT NOT NULL CHECK (response_digest ~ '^sha256:[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((credential_id IS NULL) <> (requested_by_user_id IS NULL)),
    FOREIGN KEY (workspace_id,environment_id) REFERENCES customer_kubernetes_environments(workspace_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,credential_id) REFERENCES operational_guidance_credentials(workspace_id,environment_id,id) ON DELETE RESTRICT
);
CREATE INDEX operational_guidance_requests_scope ON operational_guidance_requests(workspace_id,environment_id,created_at DESC);
CREATE TRIGGER operational_guidance_requests_immutable BEFORE UPDATE OR DELETE ON operational_guidance_requests FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_guidance_requests_no_truncate BEFORE TRUNCATE ON operational_guidance_requests FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();

CREATE TABLE operational_guidance_experiments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    dataset_id TEXT NOT NULL,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE RESTRICT,
    execution_kind TEXT NOT NULL CHECK (execution_kind IN ('offline_replay','hosted')),
    mode TEXT NOT NULL CHECK (mode IN ('without_guidance','with_guidance')),
    results JSONB NOT NULL CHECK (jsonb_typeof(results) = 'object'),
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id,dataset_id,agent_version_id,scenario_version_id,execution_kind,mode)
);
CREATE TRIGGER operational_guidance_experiments_immutable BEFORE UPDATE OR DELETE ON operational_guidance_experiments FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_guidance_experiments_no_truncate BEFORE TRUNCATE ON operational_guidance_experiments FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();
