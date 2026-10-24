-- Stage-1 verifier hardening: every scenario version declares evidence
-- applicability, and every new verification result records completeness.
ALTER TABLE scenario_versions
    ADD COLUMN evidence_requirements JSONB NOT NULL DEFAULT '{
      "contract_version":"evidence-requirements:v1",
      "workload_state":"REQUIRED",
      "telemetry":"REQUIRED",
      "git_desired_state":"NOT_APPLICABLE",
      "argo_state":"NOT_APPLICABLE",
      "stability":"REQUIRED"
    }'::jsonb
    CHECK (jsonb_typeof(evidence_requirements) = 'object');

ALTER TABLE verification_results
    ADD COLUMN evidence_assessment JSONB
    CHECK (evidence_assessment IS NULL OR jsonb_typeof(evidence_assessment) = 'object'),
    DROP CONSTRAINT verification_results_verifier_contract_version_check;

ALTER TABLE verification_results
    ADD CONSTRAINT verification_results_verifier_contract_version_check
        CHECK (verifier_contract_version IN ('infrastructure-verifier:v1','infrastructure-verifier:v2','infrastructure-verifier:v3')),
    ADD CONSTRAINT verification_results_evidence_assessment_v3_check
        CHECK (verifier_contract_version <> 'infrastructure-verifier:v3' OR evidence_assessment IS NOT NULL);

COMMENT ON COLUMN verification_results.evidence_assessment IS
    'Required/applicable/available evidence dimensions under evidence-requirements:v1. NULL only for historical pre-000024 results.';
