DROP TABLE IF EXISTS capability_coverage_assessments;
DROP TRIGGER IF EXISTS scenario_version_task_family_evidence_freeze ON scenario_version_task_family;
DROP FUNCTION IF EXISTS reject_task_family_reassignment_after_evidence();
DROP TABLE IF EXISTS scenario_version_task_family;
DROP TABLE IF EXISTS task_families;
