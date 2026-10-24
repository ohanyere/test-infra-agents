ALTER TABLE verification_results DISABLE TRIGGER verification_results_immutable;
ALTER TABLE verification_results
    DROP CONSTRAINT verification_results_verifier_contract_version_check,
    DROP CONSTRAINT verification_results_snapshot_contract_version_check,
    DROP COLUMN invariant_results;
ALTER TABLE verification_results
    -- Keep v2 contract values valid so rollback never rewrites or deletes
    -- immutable results that were created while this migration was active.
    ADD CONSTRAINT verification_results_verifier_contract_version_check
        CHECK (verifier_contract_version IN ('infrastructure-verifier:v1','infrastructure-verifier:v2')),
    ADD CONSTRAINT verification_results_snapshot_contract_version_check
        CHECK (snapshot_contract_version IN ('kubernetes-snapshot:v1','infrastructure-snapshot:v2'));
ALTER TABLE verification_results ENABLE TRIGGER verification_results_immutable;
