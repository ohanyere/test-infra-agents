-- First-class invariant projection for GitOps desired/live verification.
-- Historical results remain immutable and receive an empty invariant set only.
ALTER TABLE verification_results DISABLE TRIGGER verification_results_immutable;
ALTER TABLE verification_results
    ADD COLUMN invariant_results JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(invariant_results) = 'array'),
    DROP CONSTRAINT verification_results_verifier_contract_version_check,
    DROP CONSTRAINT verification_results_snapshot_contract_version_check;
ALTER TABLE verification_results
    ADD CONSTRAINT verification_results_verifier_contract_version_check
        CHECK (verifier_contract_version IN ('infrastructure-verifier:v1','infrastructure-verifier:v2')),
    ADD CONSTRAINT verification_results_snapshot_contract_version_check
        CHECK (snapshot_contract_version IN ('kubernetes-snapshot:v1','infrastructure-snapshot:v2'));
ALTER TABLE verification_results ENABLE TRIGGER verification_results_immutable;
