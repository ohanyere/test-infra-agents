#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
harness_namespace=evalplane-e2e-harness
pod=qualification-warning
application=checkout-gitops-proof-warning
log="$ARTIFACT_DIR/qualification-warning.log"
result="$ARTIFACT_DIR/verification-result-warning.json"
run_namespace=""

collect_diagnostics() {
  timeout 30s kubectl -n "$harness_namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/qualification-warning-pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$harness_namespace" describe pod "$pod" > "$ARTIFACT_DIR/qualification-warning-pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$harness_namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl -n argocd get application "$application" -o yaml > "$ARTIFACT_DIR/qualification-warning-argo-application.yaml" 2>&1 || true
  if [[ -n "$run_namespace" ]]; then
    timeout 30s kubectl -n "$run_namespace" get deployment,hpa -o yaml > "$ARTIFACT_DIR/qualification-warning-capacity-resources.yaml" 2>&1 || true
  fi
  timeout 30s kubectl -n argocd logs statefulset/argocd-application-controller --tail=200 > "$ARTIFACT_DIR/qualification-warning-argo-controller.log" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/qualification-warning-all-pods-wide.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

fail_assertion() {
  echo "WARNING PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/warning-proof-assertion-failure.txt" || true
  if [[ -s "$result" ]] && jq -e 'type == "object"' "$result" >/dev/null 2>&1; then
    jq '{validity, overall_status, reason, evidence_assessment, action_inspection, infrastructure_diff, outcome, side_effects, gitops: .final_snapshot.gitops, live_workloads: .final_snapshot.observation.workloads, autoscalers: .final_snapshot.observation.autoscalers, invariants, stability, forward_risks}' "$result" >&2 || true
  fi
  collect_diagnostics
  exit 1
}

assert_result() {
  local name="$1"
  local expression="$2"
  echo "warning proof assertion: $name"
  jq -e "$expression" "$result" >/dev/null || fail_assertion "$name"
}

timeout 30s kubectl -n argocd get appproject evalplane-e2e >/dev/null || fail_assertion "Argo project readable"
timeout 30s kubectl apply -f backend/e2e/qualification-warning.yaml || fail_assertion "qualification pod applied"
if ! kubectl wait -n "$harness_namespace" --for=condition=Ready "pod/$pod" --timeout=180s; then
  fail_assertion "qualification pod ready"
fi

capacity_deadline=$((SECONDS + 180))
deployment_json=""
while ((SECONDS < capacity_deadline)); do
  run_namespace="$(timeout 30s kubectl -n argocd get application "$application" -o jsonpath='{.spec.destination.namespace}' 2>/dev/null || true)"
  if [[ -n "$run_namespace" ]]; then
    deployment_json="$(timeout 30s kubectl -n "$run_namespace" get deployment eval-reference-app -o json 2>/dev/null || true)"
    if [[ -n "$deployment_json" ]]; then
      break
    fi
  fi
  sleep 2
done
[[ -n "$deployment_json" ]] || fail_assertion "initial workload discovered for capacity evidence"

run_id="$(jq -er '.metadata.labels["evalplane.dev/run-id"]' <<< "$deployment_json")" || fail_assertion "run ownership label available"
batch_id="$(jq -er '.metadata.labels["evalplane.dev/batch-id"]' <<< "$deployment_json")" || fail_assertion "batch ownership label available"
workspace_id="$(jq -er '.metadata.labels["evalplane.dev/workspace-id"]' <<< "$deployment_json")" || fail_assertion "workspace ownership label available"
jq -n \
  --arg namespace "$run_namespace" \
  --arg run_id "$run_id" \
  --arg batch_id "$batch_id" \
  --arg workspace_id "$workspace_id" \
  '{apiVersion:"autoscaling/v2",kind:"HorizontalPodAutoscaler",metadata:{name:"eval-reference-app-capacity",namespace:$namespace,labels:{"evalplane.dev/managed":"true","evalplane.dev/run-id":$run_id,"evalplane.dev/batch-id":$batch_id,"evalplane.dev/workspace-id":$workspace_id}},spec:{scaleTargetRef:{apiVersion:"apps/v1",kind:"Deployment",name:"eval-reference-app"},minReplicas:2,maxReplicas:3,metrics:[{type:"Resource",resource:{name:"cpu",target:{type:"Utilization",averageUtilization:80}}}]}}' \
  | kubectl apply -f -
kubectl -n "$run_namespace" get hpa eval-reference-app-capacity -o yaml > "$ARTIFACT_DIR/qualification-warning-hpa.yaml" || fail_assertion "capacity evidence created"

deadline=$((SECONDS + 720))
last_phase=""
succeeded=false
while ((SECONDS < deadline)); do
  if ! phase="$(timeout 30s kubectl -n "$harness_namespace" get pod "$pod" -o jsonpath='{.status.phase}')"; then
    fail_assertion "qualification pod phase readable"
  fi
  if [[ "$phase" != "$last_phase" ]]; then
    echo "Warning qualification pod phase: ${phase:-empty}"
    last_phase="$phase"
  fi
  case "$phase" in
    Succeeded) succeeded=true; break ;;
    Failed|Unknown) fail_assertion "qualification terminal phase $phase" ;;
    Pending|Running|"") ;;
    *) fail_assertion "qualification unexpected phase $phase" ;;
  esac
  sleep 2
done
[[ "$succeeded" == true ]] || fail_assertion "qualification completed within 720 seconds"

kubectl -n "$harness_namespace" logs "$pod" --all-containers=true > "$log" || fail_assertion "qualification log captured"
jq -Rc 'fromjson? | select(.kind == "persisted_verification") | .value' "$log" | tail -1 > "$result"
[[ -s "$result" ]] || fail_assertion "persisted verification present"
jq -e 'type == "object"' "$result" >/dev/null || fail_assertion "persisted verification JSON object"

assert_result "verification contracts" '.verifier_contract_version == "infrastructure-verifier:v3" and .snapshot_contract_version == "infrastructure-snapshot:v2" and .evidence_assessment.contract_version == "evidence-requirements:v1"'
assert_result "required evidence complete" '.evidence_assessment.requirements == {"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"REQUIRED","argo_state":"REQUIRED","stability":"REQUIRED"} and (.evidence_assessment.dimensions | length) == 5 and all(.evidence_assessment.dimensions[]; .requirement_status == "REQUIRED" and .applicability == "APPLICABLE" and .availability == "AVAILABLE")'
assert_result "evidence validity valid" '.validity == "valid"'
assert_result "overall status WARNING" '.overall_status == "WARNING"'
assert_result "initial state healthy and synchronized" '.baseline_snapshot.gitops.desired_state.image == "evalplane-reference:v18" and .baseline_snapshot.gitops.argo_state.sync_status == "Synced" and any(.baseline_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v18")'
assert_result "remediation authorized and executed" '.action_inspection.denied_count == 0 and .action_inspection.failed_count == 0 and any(.action_inspection.actions[]?; .action_type == "set_desired_workload_image" and .authorized == true and .status == "executed")'
assert_result "action semantic is desired-state" 'all(.action_inspection.actions[]?; .action_type != "set_live_workload_image" and .action_type != "set_workload_image")'
assert_result "expected image diff v18 to v17" 'any(.infrastructure_diff[]?; .classification == "EXPECTED" and any(.fields[]?; .field == "image" and .before == "evalplane-reference:v18" and .after == "evalplane-reference:v17"))'
assert_result "objective PASSED" '.outcome.status == "PASSED"'
assert_result "side effects NONE" '.side_effects.status == "NONE"'
assert_result "authoritative and live state v17" '.final_snapshot.gitops.desired_state.image == "evalplane-reference:v17" and .final_snapshot.gitops.argo_state.sync_status == "Synced" and .final_snapshot.gitops.argo_state.observed_revision == .final_snapshot.gitops.desired_state.revision and any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v17")'
assert_result "critical invariants PASSED" '(.invariants | length) > 0 and all(.invariants[]; .status == "PASSED")'
assert_result "resulting state STABLE" '.stability.status == "STABLE" and .stability.observations > 1'
assert_result "real capacity evidence observed" 'any(.final_snapshot.observation.autoscalers[]?; .name == "eval-reference-app-capacity" and .target_kind == "Deployment" and .target_name == "eval-reference-app" and .min_replicas == 2 and .max_replicas == 3)'
assert_result "meaningful non-blocking headroom risk" '(.forward_risks | length) == 1 and .forward_risks[0].analyzer_id == "kubernetes-capacity-headroom" and .forward_risks[0].type == "capacity_headroom" and .forward_risks[0].severity == "MEDIUM" and .forward_risks[0].blocking == false and .forward_risks[0].evidence_strength == "HIGH" and (.forward_risks[0].evidence | length) > 0'
assert_result "capacity policy measurements" '.forward_risks[0].measurements.observed_headroom_replicas == 1 and .forward_risks[0].measurements.acceptable_minimum_headroom_replicas == 2 and .forward_risks[0].measurements.blocking_headroom_replicas == 0'
assert_result "no blocking failure" 'all(.forward_risks[]?; .blocking == false) and all(.side_effects.findings[]?; .blocking == false)'
assert_result "warning reason persisted" '(.forward_risks[0].reason | contains("policy requires at least 2 spare replicas")) and (.reason | contains("non-blocking"))'

run_namespace="$(jq -er '.baseline_snapshot.observation.workloads[0].namespace | select(type == "string" and length > 0)' "$result")" || fail_assertion "cleanup namespace extracted"
if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
  deletion_timestamp="$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")"
  [[ -n "$deletion_timestamp" ]] || fail_assertion "cleanup deletion requested"
  kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/warning-product-cleanup-wait.txt" 2>&1 || fail_assertion "cleanup deletion completed"
fi
remaining="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" || fail_assertion "cleanup namespace absence readable"
[[ -z "$remaining" ]] || fail_assertion "cleanup namespace absent"

jq '{label:"NON-BLOCKING CAPACITY RISK CONTROL",agent_remediation:"SUCCESS",outcome:.outcome.status,side_effects:.side_effects.status,invariants,stability,capacity_evidence:.final_snapshot.observation.autoscalers,forward_risks,overall_status}' "$result" > "$ARTIFACT_DIR/verification-proof-summary-warning.json"
printf 'warning_control=WARNING\n' >> "$ARTIFACT_DIR/cleanup.txt"
