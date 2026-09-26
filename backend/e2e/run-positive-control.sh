#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
namespace=evalplane-e2e-harness
pod=qualification-positive
log="$ARTIFACT_DIR/qualification-positive.log"
result="$ARTIFACT_DIR/verification-result-positive.json"

collect_diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/qualification-positive-pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/qualification-positive-pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl -n argocd get application checkout-gitops-proof-positive -o yaml > "$ARTIFACT_DIR/qualification-positive-argo-application.yaml" 2>&1 || true
  timeout 30s kubectl -n argocd logs statefulset/argocd-application-controller --tail=200 > "$ARTIFACT_DIR/qualification-positive-argo-controller.log" 2>&1 || true
  timeout 30s kubectl -n argocd logs deployment/argocd-repo-server --tail=200 > "$ARTIFACT_DIR/qualification-positive-argo-repo-server.log" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/qualification-positive-all-pods-wide.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

fail_assertion() {
  echo "POSITIVE PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/positive-proof-assertion-failure.txt" || true
  if [[ -s "$result" ]] && jq -e 'type == "object"' "$result" >/dev/null 2>&1; then
    jq '{validity, overall_status, evidence_assessment, action_inspection, infrastructure_diff, outcome, side_effects, gitops: .final_snapshot.gitops, live_workloads: .final_snapshot.observation.workloads, invariants, stability, forward_risks}' "$result" >&2 || true
  fi
  collect_diagnostics
  exit 1
}

assert_result() {
  local name="$1"
  local expression="$2"
  echo "positive proof assertion: $name"
  jq -e "$expression" "$result" >/dev/null || fail_assertion "$name"
}

assert_event() {
  local name="$1"
  local expression="$2"
  echo "positive proof assertion: $name"
  jq -Re "$expression" "$log" >/dev/null || fail_assertion "$name"
}

timeout 30s kubectl -n argocd get appproject evalplane-e2e >/dev/null || fail_assertion "Argo project readable"
timeout 30s kubectl apply -f backend/e2e/qualification-positive.yaml || fail_assertion "qualification pod applied"
if ! kubectl wait -n "$namespace" --for=condition=Ready "pod/$pod" --timeout=180s; then
  fail_assertion "qualification pod ready"
fi

deadline=$((SECONDS + 720))
last_phase=""
succeeded=false
while ((SECONDS < deadline)); do
  if ! phase="$(timeout 30s kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.status.phase}')"; then
    fail_assertion "qualification pod phase readable"
  fi
  if [[ "$phase" != "$last_phase" ]]; then
    echo "Positive qualification pod phase: ${phase:-empty}"
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

kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" || fail_assertion "qualification log captured"
jq -Rc 'fromjson? | select(.kind == "persisted_verification") | .value' "$log" | tail -1 > "$result"
[[ -s "$result" ]] || fail_assertion "persisted verification present"
jq -e 'type == "object"' "$result" >/dev/null || fail_assertion "persisted verification JSON object"

assert_result "verification contracts" '.verifier_contract_version == "infrastructure-verifier:v3" and .snapshot_contract_version == "infrastructure-snapshot:v2" and .evidence_assessment.contract_version == "evidence-requirements:v1"'
assert_result "required evidence complete" '.evidence_assessment.requirements == {"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"REQUIRED","argo_state":"REQUIRED","stability":"REQUIRED"} and (.evidence_assessment.dimensions | length) == 5 and all(.evidence_assessment.dimensions[]; .requirement_status == "REQUIRED" and .applicability == "APPLICABLE" and .availability == "AVAILABLE")'
assert_result "evidence validity valid" '.validity == "valid"'
assert_result "overall status VERIFIED" '.overall_status == "VERIFIED"'
assert_result "initial Git desired image v18" '.baseline_snapshot.gitops.desired_state.image == "evalplane-reference:v18"'
assert_result "initial live image v18" 'any(.baseline_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v18")'
assert_result "initial Argo Synced" '.baseline_snapshot.gitops.argo_state.sync_status == "Synced"'
assert_result "remediation authorized and executed" 'any(.action_inspection.actions[]?; .action_type == "set_workload_image" and .authorized == true and .status == "executed")'
assert_result "expected image diff v18 to v17" 'any(.infrastructure_diff[]?; .classification == "EXPECTED" and any(.fields[]?; .field == "image" and .before == "evalplane-reference:v18" and .after == "evalplane-reference:v17"))'
assert_result "immediate outcome PASSED" '.outcome.status == "PASSED"'
assert_result "side effects NONE" '.side_effects.status == "NONE"'
assert_result "Git desired image v17" '.final_snapshot.gitops.desired_state.image == "evalplane-reference:v17"'
assert_result "live Kubernetes image v17" 'any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v17")'
assert_result "Argo Synced" '.final_snapshot.gitops.argo_state.sync_status == "Synced"'
assert_result "Argo observed authoritative revision" '.final_snapshot.gitops.argo_state.observed_revision == .final_snapshot.gitops.desired_state.revision'
assert_result "desired/live invariant PASSED" 'any(.invariants[]?; .invariant_type == "DESIRED_LIVE_STATE_CONSISTENCY" and .status == "PASSED")'
assert_result "stability STABLE" '.stability.status == "STABLE" and .stability.observations > 1'
assert_result "no forward risks" '(.forward_risks | length) == 0'
assert_event "gateway boundary" 'fromjson? | select(.kind == "gateway_boundary" and .value.checked == true and .value.requests > 0)'
assert_event "action proposed persisted" 'fromjson? | select(.kind == "persisted_action_proposed")'
assert_event "action transition persisted" 'fromjson? | select(.kind == "persisted_action_transition")'

run_namespace="$(jq -er '.baseline_snapshot.observation.workloads[0].namespace | select(type == "string" and length > 0)' "$result")" || fail_assertion "cleanup namespace extracted"
if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
  deletion_timestamp="$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")"
  [[ -n "$deletion_timestamp" ]] || fail_assertion "cleanup deletion requested"
  kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/positive-product-cleanup-wait.txt" 2>&1 || fail_assertion "cleanup deletion completed"
fi
remaining="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" || fail_assertion "cleanup namespace absence readable"
[[ -z "$remaining" ]] || fail_assertion "cleanup namespace absent"

jq '{label:"POSITIVE DURABLE GITOPS CONTROL", agent_remediation:"SUCCESS", immediate_outcome:.outcome.status, side_effects:.side_effects.status, git_desired_image:.final_snapshot.gitops.desired_state.image, live_images:[.final_snapshot.observation.workloads[]? | select(.name == "eval-reference-app") | .image], argo_sync:.final_snapshot.gitops.argo_state.sync_status, invariants, stability, forward_risks, overall_status}' "$result" > "$ARTIFACT_DIR/verification-proof-summary-positive.json"
printf 'positive_control=VERIFIED\n' >> "$ARTIFACT_DIR/cleanup.txt"
