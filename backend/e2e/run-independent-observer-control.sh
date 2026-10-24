#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?artifact directory required}"
: "${DATABASE_URL:?database required}"
namespace=realverax-audit-proof
agent_principal=system:serviceaccount:${namespace}:direct-agent
observer_principal=system:serviceaccount:${namespace}:independent-observer
mkdir -p "$ARTIFACT_DIR"

kubectl apply -f backend/e2e/independent-observer.yaml
for deployment in direct-app forbidden-app false-claim-app; do
  kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=180s
done

test "$(kubectl auth can-i patch deployments --resource-name=direct-app -n "$namespace" --as="$agent_principal")" = yes
test "$(kubectl auth can-i patch deployments --resource-name=forbidden-app -n "$namespace" --as="$agent_principal")" = no
test "$(kubectl auth can-i patch deployments --resource-name=false-claim-app -n "$namespace" --as="$agent_principal")" = no
test "$(kubectl auth can-i patch deployments --resource-name=direct-app -n "$namespace" --as="$observer_principal")" = no
test "$(kubectl auth can-i get deployments -n "$namespace" --as="$observer_principal")" = yes

observer_config="$(mktemp)"
chmod 600 "$observer_config"
trap 'rm -f "$observer_config"' EXIT
observer_token="$(kubectl -n "$namespace" create token independent-observer --duration=1h)"
echo "::add-mask::$observer_token"
cluster_server="$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
cluster_ca="$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
test -n "$cluster_server"
test -n "$cluster_ca"
cat > "$observer_config" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: independent-kind
    cluster:
      server: $cluster_server
      certificate-authority-data: $cluster_ca
users:
  - name: independent-observer
    user:
      token: $observer_token
contexts:
  - name: independent-kind
    context:
      cluster: independent-kind
      user: independent-observer
current-context: independent-kind
EOF
export OBSERVER_KUBECONFIG="$observer_config"
test "$(kubectl --kubeconfig "$observer_config" auth whoami -o jsonpath='{.status.userInfo.username}')" = "$observer_principal"
if kubectl --kubeconfig "$observer_config" -n "$namespace" patch deployment direct-app -p '{"spec":{"replicas":5}}' --dry-run=server >/dev/null 2>&1; then
  echo 'read-only observer unexpectedly authorized to mutate' >&2
  exit 1
fi

( cd backend && GOMAXPROCS=2 go build -o /tmp/independentqualification ./cmd/independentqualification )

run_control() {
  local control="$1" deployment="$2" expected="$3"
  /tmp/independentqualification -mode snapshot -name "$deployment" > "$ARTIFACT_DIR/independent-${control}-baseline.json"
  kubectl apply -f backend/e2e/independent-agent-jobs.yaml -l "independent-control=$control"
  kubectl -n "$namespace" wait --for=condition=complete "job/direct-agent-$control" --timeout=90s
  kubectl -n "$namespace" logs "job/direct-agent-$control" > "$ARTIFACT_DIR/independent-${control}-agent-claim.json"
  if [[ "$control" == positive ]]; then
    kubectl -n "$namespace" rollout status deployment/direct-app --timeout=180s
  fi
  /tmp/independentqualification -mode verify -name "$deployment" \
    -baseline "$ARTIFACT_DIR/independent-${control}-baseline.json" \
    -report "$ARTIFACT_DIR/independent-${control}-agent-claim.json" \
    -job "direct-agent-$control" > "$ARTIFACT_DIR/independent-${control}-verification.json"
  jq -e --arg expected "$expected" '.overall_status == $expected and .baseline_snapshot.gitops == null and .final_snapshot.gitops == null and (.provenance.providers | any(.role == "workload_state" and .provider_id == "kubernetes-api:read-only-serviceaccount"))' "$ARTIFACT_DIR/independent-${control}-verification.json" >/dev/null
}

run_control positive direct-app VERIFIED
jq -e '.action_inspection.attribution.status == "ATTRIBUTED" and .action_inspection.executed_count == 1 and .action_inspection.denied_count == 0 and (.evidence_assessment.dimensions | any(.evidence_type == "kubernetes_audit" and .availability == "AVAILABLE")) and (.final_snapshot.observation.workloads | any(.name == "direct-app" and .desired_replicas == 5 and .ready_replicas == 5))' "$ARTIFACT_DIR/independent-positive-verification.json" >/dev/null

run_control forbidden forbidden-app FAILED
jq -e --slurpfile before "$ARTIFACT_DIR/independent-forbidden-baseline.json" '.action_inspection.denied_count == 1 and .action_inspection.executed_count == 0 and .action_inspection.actions[0].result.response_code == 403 and (.final_snapshot.observation.workloads | any(.name == "forbidden-app" and .desired_replicas == 3 and .ready_replicas == 3 and .uid == $before[0].workloads[0].uid and .generation == $before[0].workloads[0].generation))' "$ARTIFACT_DIR/independent-forbidden-verification.json" >/dev/null

run_control false-claim false-claim-app INCONCLUSIVE
jq -e --slurpfile before "$ARTIFACT_DIR/independent-false-claim-baseline.json" '.action_inspection.attribution.status == "INCONCLUSIVE" and .action_inspection.executed_count == 0 and (.evidence_assessment.dimensions | any(.evidence_type == "kubernetes_audit" and .availability == "UNAVAILABLE")) and (.final_snapshot.observation.workloads | any(.name == "false-claim-app" and .desired_replicas == 3 and .ready_replicas == 3 and .uid == $before[0].workloads[0].uid and .generation == $before[0].workloads[0].generation))' "$ARTIFACT_DIR/independent-false-claim-verification.json" >/dev/null

printf 'independent_observer_control=VERIFIED\n' > "$ARTIFACT_DIR/independent-proof-summary.txt"
