# Customer audit export forwarder

Run this process in the customer's security domain with a **read-only export** of Kubernetes API-server audit JSON lines. It requires `AUDIT_LOG_PATH`, `REALVERAX_API_URL` (HTTPS), `OBSERVATION_SESSION_ID`, `REGISTERED_CLUSTER_ID`, and the one-time issued `COLLECTOR_CREDENTIAL`. The environment must have been registered with `allow_forwarded_audit: true`.

The process reads the export once. Re-running it after interruption is safe because Realverax deduplicates by registered environment and audit event ID, and rejects contradictory retries. It retries transient HTTP failures three times. It has no Kubernetes write operation or kubeconfig requirement.

Kubernetes audit logs are normally configured at API-server startup. Providing a file export may require cluster-operator access to control-plane configuration or an audit webhook. A pod with ordinary read-only Kubernetes RBAC cannot read API-server audit logs. The registration identifier and collector credential authenticate the forwarder to Realverax; they do **not** authenticate the record as originating from that API server. Forwarded records retain `INSUFFICIENT_PROVENANCE` and cannot establish agent attribution.
