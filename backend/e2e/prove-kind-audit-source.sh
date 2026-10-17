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

node=evalplane-e2e-control-plane
manifest="$(docker exec "$node" cat /etc/kubernetes/manifests/kube-apiserver.yaml)"
report_manifest_flag() {
  local flag="$1" expected="$2"
  if awk -v flag="--$flag" -v expected="$expected" '
    {
      value = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", value)
      if (value == flag "=" expected || (previous == flag && value == expected)) found = 1
      previous = value
    }
    END { exit !found }
  ' <<<"$manifest"; then
    printf 'audit diagnostic: static-manifest %s=expected\n' "$flag"
  else
    printf 'audit diagnostic: static-manifest %s=missing-or-mismatched\n' "$flag"
  fi
}
report_manifest_flag audit-policy-file /etc/kubernetes/policies/audit-policy.yaml
report_manifest_flag audit-log-path /var/log/kubernetes/audit/audit.log

report_check() {
  local name="$1"; shift
  if "$@"; then printf 'audit diagnostic: %s=accessible\n' "$name"; else printf 'audit diagnostic: %s=unavailable\n' "$name"; fi
}
container_ids="$(docker exec "$node" crictl ps --name kube-apiserver -q)"
if [[ "$(wc -w <<<"$container_ids")" -eq 1 ]]; then
  container_id="${container_ids%%$'\n'*}"
  mapfile -t process_args < <(docker exec "$node" crictl inspect "$container_id" | jq -r '.info.runtimeSpec.process.args[]?')
  report_process_flag() {
    local flag="$1" expected="$2" found=false
    for ((i = 0; i < ${#process_args[@]}; i++)); do
      if [[ "${process_args[i]}" == "--$flag=$expected" ]] || \
        [[ "${process_args[i]}" == "--$flag" && "${process_args[i+1]:-}" == "$expected" ]]; then
        found=true
      fi
    done
    printf 'audit diagnostic: running-process %s=%s\n' "$flag" "$([[ "$found" == true ]] && echo expected || echo missing-or-mismatched)"
  }
  report_process_flag audit-policy-file /etc/kubernetes/policies/audit-policy.yaml
  report_process_flag audit-log-path /var/log/kubernetes/audit/audit.log
  report_check api-server-policy-readable docker exec "$node" crictl exec "$container_id" test -r /etc/kubernetes/policies/audit-policy.yaml
  report_check api-server-audit-directory-writable docker exec "$node" crictl exec "$container_id" test -w /var/log/kubernetes/audit
else
  echo "audit diagnostic: running-process kube-apiserver-container-count=$(wc -w <<<"$container_ids")"
fi

report_check policy-file docker exec "$node" test -r /etc/kubernetes/policies/audit-policy.yaml
report_check audit-log-directory-writable docker exec "$node" test -w /var/log/kubernetes/audit
mounts="$(docker inspect "$node" --format '{{json .Mounts}}')"
for mount in /etc/kubernetes/policies/audit-policy.yaml /var/log/kubernetes/audit; do
  if jq -e --arg mount "$mount" 'any(.[]; .Destination == $mount)' <<<"$mounts" >/dev/null; then
    printf 'audit diagnostic: kind-node-mount %s=present\n' "$mount"
  else
    printf 'audit diagnostic: kind-node-mount %s=missing\n' "$mount"
  fi
done
for mount in /etc/kubernetes/policies/audit-policy.yaml /var/log/kubernetes/audit; do
  if grep -Fq "mountPath: $mount" <<<"$manifest"; then
    printf 'audit diagnostic: api-server-volume-mount %s=present\n' "$mount"
  else
    printf 'audit diagnostic: api-server-volume-mount %s=missing\n' "$mount"
  fi
done

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
