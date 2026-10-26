-- Existing immutable observations or registered sources of the new kinds make
-- rollback unsafe. PostgreSQL rejects this constraint instead of deleting data.
ALTER TABLE customer_direct_outcome_observations DROP CONSTRAINT customer_direct_outcome_observations_source_kind_check;
ALTER TABLE customer_direct_outcome_observations ADD CONSTRAINT customer_direct_outcome_observations_source_kind_check
    CHECK (source_kind IN ('kubernetes_state','argocd','prometheus'));
ALTER TABLE customer_outcome_sources DROP CONSTRAINT customer_outcome_sources_source_kind_check;
ALTER TABLE customer_outcome_sources ADD CONSTRAINT customer_outcome_sources_source_kind_check
    CHECK (source_kind IN ('kubernetes_state','argocd','prometheus'));
