// Package gitopsqual owns the V1 GitOps and progressive-delivery
// qualification catalog: built-in Evaluation Task definitions that distinguish
// live state, desired state, GitOps controller state, delivery state,
// telemetry, and dependency health.
//
// These are seed definitions over the existing scenario model
// (scenario.Version + coverage family/case assignment + environment
// requirements + evaluator criteria + evaluator-only reference remediation).
// There is deliberately no second runtime, no second grader, and no second
// task API:
//
//   - execution still flows through the environment scheduler; GitOps tasks
//     require gitops.argocd:v1 components and progressive tasks require
//     argo_rollouts, neither of which has an active provider, so every task
//     in this package truthfully reports runnable=false / not_available
//     until a real provider exists (see Availability);
//   - grading is the deterministic evaluator (deterministic-evaluator:v1);
//   - task trust is scenario certification
//     (scenario-certification-contract:v1) via focused fake validation;
//   - breadth is capability coverage (capability-coverage-contract:v1);
//   - serving reuses GET /v1/qualification-tasks (httpapi aggregates this
//     package with srequal; no second GitOps task API exists).
//
// No LLM is used anywhere in this package. All matching, grading, and
// certification inputs are deterministic. No live Argo CD, Git server, or
// rollout controller is contacted: fixtures.go provides the smallest safe
// deterministic in-memory development representations (isolated per
// evaluation, exact revision, deterministic reset, no credentials), and
// catalog tasks fake-certify against them without a live cluster.
//
// Catalog truthfulness rules enforced by tests:
//
//   - no task is marked runnable merely because catalog definitions exist;
//   - gitops.argocd:v1 remains planned; Availability() derives from the
//     environment scheduler, never from a hardcoded flag;
//   - evaluator-only data (desired-state owner, root-cause layer, expected
//     fix, reference remediation, hidden variant values, decoy truth,
//     certification internals) never appears in agent-visible surfaces;
//   - no raw credentials, no cluster-admin, no arbitrary shell/git access.
package gitopsqual
