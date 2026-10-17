#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
namespace=realverax-audit-proof
name="audit-proof-${GITHUB_RUN_ID}"
mkdir -p "$ARTIFACT_DIR"
trap 'kubectl delete namespace "$namespace" --ignore-not-found --wait=false >/dev/null 2>&1 || true' EXIT

if timeout 30s kubectl get namespace "$namespace" >/dev/null 2>&1; then
  timeout 70s kubectl delete namespace "$namespace" --wait=true --timeout=60s
fi
timeout 30s kubectl create namespace "$namespace"
timeout 30s kubectl -n "$namespace" create deployment "$name" --image=registry.k8s.io/pause:3.10
timeout 30s kubectl -n "$namespace" annotate deployment "$name" "realverax.io/audit-proof=${GITHUB_RUN_ID}"
principal="$(timeout 30s kubectl auth whoami -o jsonpath='{.status.userInfo.username}')"
test -n "$principal"

cd backend
timeout 180s go run ./cmd/kubeauditproof -namespace "$namespace" -name "$name" -verb patch -principal "$principal" | tee "$ARTIFACT_DIR/kind-audit-proof.json"
jq -e --arg namespace "$namespace" --arg name "$name" --arg principal "$principal" --arg run "$GITHUB_RUN_ID" '
  .status == "runner_bound_audit_acquired" and
  .source.source_type == "kubernetes-apiserver-audit" and
  .source.cluster_identity == "kind:evalplane-e2e" and
  (.source.configuration_identity | startswith("kind-audit-policy:sha256:")) and
  .source.verification_state == "runner_bound_test_source" and
  (.source.provisioning_identity | contains($run)) and
  .event.apiVersion == "audit.k8s.io/v1" and
  .event.stage == "ResponseComplete" and
  .event.verb == "patch" and
  .event.objectRef.apiGroup == "apps" and
  .event.objectRef.apiVersion == "v1" and
  .event.objectRef.resource == "deployments" and
  .event.objectRef.namespace == $namespace and
  .event.objectRef.name == $name and
  .event.user.username == $principal and
  (.event.responseStatus.code >= 200 and .event.responseStatus.code < 300) and
  (.event.auditID | length > 0)
' "$ARTIFACT_DIR/kind-audit-proof.json" >/dev/null
echo "Kubernetes API-server audit source proof: PASS"
