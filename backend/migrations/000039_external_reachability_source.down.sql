-- This rollback is supported only before external_reachability sources or
-- immutable observations exist. PostgreSQL rejects incompatible retained data.
ALTER TABLE customer_direct_outcome_observations DROP CONSTRAINT customer_direct_outcome_observations_source_kind_check;
ALTER TABLE customer_direct_outcome_observations ADD CONSTRAINT customer_direct_outcome_observations_source_kind_check
    CHECK (source_kind IN ('kubernetes_state','kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy','argocd','prometheus'));
ALTER TABLE customer_outcome_sources DROP CONSTRAINT customer_outcome_sources_source_kind_check;
ALTER TABLE customer_outcome_sources ADD CONSTRAINT customer_outcome_sources_source_kind_check
    CHECK (source_kind IN ('kubernetes_state','kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy','argocd','prometheus'));
