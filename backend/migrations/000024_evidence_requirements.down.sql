ALTER TABLE verification_results
    DROP CONSTRAINT verification_results_verifier_contract_version_check,
    DROP CONSTRAINT verification_results_evidence_assessment_v3_check,
    DROP COLUMN evidence_assessment;

-- Keep v3 valid during rollback so immutable results are never rewritten.
ALTER TABLE verification_results
    ADD CONSTRAINT verification_results_verifier_contract_version_check
        CHECK (verifier_contract_version IN ('infrastructure-verifier:v1','infrastructure-verifier:v2','infrastructure-verifier:v3'));

ALTER TABLE scenario_versions
    DROP COLUMN evidence_requirements;
