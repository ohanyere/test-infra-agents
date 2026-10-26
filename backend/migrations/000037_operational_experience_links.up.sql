ALTER TABLE operational_lesson_decisions DROP CONSTRAINT operational_lesson_decisions_decision_check;
ALTER TABLE operational_lesson_decisions ADD CONSTRAINT operational_lesson_decisions_decision_check CHECK (decision IN ('approved','rejected','revoked','retired'));

ALTER TABLE operational_lessons ADD COLUMN supersedes_lesson_id UUID;
ALTER TABLE operational_lessons ADD CONSTRAINT operational_lessons_supersedes_scope_fkey
    FOREIGN KEY (workspace_id,environment_id,supersedes_lesson_id)
    REFERENCES operational_lessons(workspace_id,environment_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX operational_lessons_one_successor ON operational_lessons(supersedes_lesson_id) WHERE supersedes_lesson_id IS NOT NULL;
ALTER TABLE operational_guidance_requests ADD CONSTRAINT operational_guidance_requests_link_scope_key UNIQUE (workspace_id,environment_id,id);

-- The reference is carried in the gateway's signed receipt after mTLS agent
-- authentication. It proves that the authenticated execution supplied this
-- response ID, not that the agent read or followed the response.
CREATE TABLE operational_guidance_operation_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    environment_id UUID NOT NULL,
    session_id UUID NOT NULL,
    run_id UUID NOT NULL,
    guidance_request_id UUID NOT NULL,
    execution_receipt_id UUID NOT NULL,
    agent_version_id UUID NOT NULL REFERENCES agent_versions(id) ON DELETE RESTRICT,
    relation TEXT NOT NULL DEFAULT 'reference_supplied_to_gateway'
        CHECK (relation = 'reference_supplied_to_gateway'),
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,environment_id,guidance_request_id)
        REFERENCES operational_guidance_requests(workspace_id,environment_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id,environment_id,session_id,run_id,execution_receipt_id)
        REFERENCES customer_execution_receipts(workspace_id,environment_id,session_id,run_id,id) ON DELETE RESTRICT,
    UNIQUE (execution_receipt_id),
    UNIQUE (workspace_id,environment_id,id)
);
CREATE INDEX operational_guidance_operation_links_scope ON operational_guidance_operation_links(workspace_id,environment_id,guidance_request_id);
CREATE TRIGGER operational_guidance_operation_links_immutable BEFORE UPDATE OR DELETE ON operational_guidance_operation_links FOR EACH ROW EXECUTE FUNCTION prevent_operational_history_change();
CREATE TRIGGER operational_guidance_operation_links_no_truncate BEFORE TRUNCATE ON operational_guidance_operation_links FOR EACH STATEMENT EXECUTE FUNCTION prevent_operational_history_change();
