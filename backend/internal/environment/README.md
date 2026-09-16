# Environment profiles & composition (V1)

Composable model: components → profiles → providers → scheduler → provisioned
`EnvironmentInstance`. Architecture/model/scheduling layer only: no AWS, EKS,
vCluster, Terraform, or cloud control plane is implemented.

## Contracts

- `environment-component-catalog:v1` (components)
- `environment-profile-catalog:v1` (profiles)
- `environment-provider-contract:v1` (providers)
- `environment-scheduler-contract:v1` (matching)
- `environment-requirements:v1` (per-ScenarioVersion requirements)

Historical runs/certifications pin profile/provider/runtime identities so
future catalog changes never rewrite history.

## Isolation tiers

`namespace < virtual_cluster < dedicated_cluster`. A stronger profile may
satisfy a weaker minimum; a weaker profile can never satisfy a stronger
requirement, and the scheduler never downgrades fidelity for availability
(unavailable dedicated provider → `no_eligible_environment`, never
namespace fallback).

- `namespace`: shared control plane, workload isolation through
  namespace/RBAC/network/quota.
- `virtual_cluster`: isolated logical Kubernetes control plane on shared
  physical infrastructure.
- `dedicated_cluster`: independent Kubernetes cluster lifecycle.

## Availability truth (V1)

- `kubernetes.namespace:v1` (active): current development runtime.
- `kubernetes.virtual_cluster:v1`, `kubernetes.dedicated:v1`,
  `gitops.argocd:v1`: planned/experimental, never auto-selected.
- `kind-dev:v1`: development-only provider wrapping the existing `kubeenv`
  manager. Not production qualification infrastructure.
- `shared-eks`, `vcluster-eks`, `dedicated-eks`, `aws-sandbox`: planned
  only, no fake health. No AWS/GCP/Terraform code exists.

## Instance lifecycle

`provisioning → ready → resetting → ready | destroying → destroyed`, with
`failed` as a side terminal. `destroyed` never returns to `ready`; repeated
destroy is idempotent. Every instance belongs to a workspace and one
execution owner (`run` or `certification`); instances are never shared
across tenants. `provider_ref` is a safe opaque handle only: no kubeconfigs,
tokens, or cloud credentials are stored or returned. Agents reach clusters
only through mediated HTTP/MCP/tool paths.
