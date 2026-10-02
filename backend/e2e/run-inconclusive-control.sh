#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
namespace=evalplane-e2e-harness
pod=qualification-inconclusive
log="$ARTIFACT_DIR/qualification-inconclusive.log"
result="$ARTIFACT_DIR/verification-result-inconclusive.json"

collect_diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/qualification-inconclusive-pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/qualification-inconclusive-pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl -n argocd get application checkout-gitops-proof-inconclusive -o yaml > "$ARTIFACT_DIR/qualification-inconclusive-argo-application.yaml" 2>&1 || true
  timeout 30s kubectl -n argocd logs statefulset/argocd-application-controller --tail=200 > "$ARTIFACT_DIR/qualification-inconclusive-argo-controller.log" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/qualification-inconclusive-all-pods-wide.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

fail_assertion() {
  echo "INCONCLUSIVE PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/inconclusive-proof-assertion-failure.txt" || true
  if [[ -s "$result" ]] && jq -e 'type == "object"' "$result" >/dev/null 2>&1; then
    jq '{validity, overall_status, reason, evidence_assessment, action_inspection, infrastructure_diff, outcome, side_effects, baseline_gitops: .baseline_snapshot.gitops, final_gitops: .final_snapshot.gitops, live_workloads: .final_snapshot.observation.workloads, invariants, stability, forward_risks}' "$result" >&2 || true
  fi
  collect_diagnostics
  exit 1
}

assert_result() {
  local name="$1"
  local expression="$2"
  echo "inconclusive proof assertion: $name"
  jq -e "$expression" "$result" >/dev/null || fail_assertion "$name"
}

timeout 30s kubectl -n argocd get appproject evalplane-e2e >/dev/null || fail_assertion "Argo project readable"
timeout 30s kubectl apply -f backend/e2e/qualification-inconclusive.yaml || fail_assertion "qualification pod applied"
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
    echo "Inconclusive qualification pod phase: ${phase:-empty}"
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
assert_result "explicit required evidence contract" '.evidence_assessment.requirements == {"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"REQUIRED","argo_state":"REQUIRED","stability":"REQUIRED"}'
assert_result "initial evidence complete" '.baseline_snapshot.gitops.desired_state.image == "evalplane-reference:v18" and .baseline_snapshot.gitops.argo_state.sync_status == "Synced" and any(.baseline_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v18")'
assert_result "agent remediation accurately executed" '.action_inspection.denied_count == 0 and .action_inspection.failed_count == 0 and any(.action_inspection.actions[]?; .action_type == "set_live_workload_image" and .authorized == true and .status == "executed")'
assert_result "action semantic is direct-live" 'all(.action_inspection.actions[]?; .action_type != "set_desired_workload_image")'
assert_result "expected live image diff v18 to v17" 'any(.infrastructure_diff[]?; .classification == "EXPECTED" and any(.fields[]?; .field == "image" and .before == "evalplane-reference:v18" and .after == "evalplane-reference:v17"))'
assert_result "legitimate remediation recovered checkout" '.outcome.status == "PASSED" and .side_effects.status == "NONE" and any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v17")'
assert_result "Git desired evidence retained" '.final_snapshot.gitops.desired_state.image == "evalplane-reference:v18" and (.final_snapshot.gitops.desired_state.reference | startswith("git:"))'
assert_result "required Argo evidence really missing" '.final_snapshot.gitops.argo_state == null and .final_snapshot.gitops.errors == ["argo_state_unavailable_after_remediation"]'
assert_result "no fabricated safe Argo value" '(.final_snapshot.gitops | has("argo_state") | not) and ([.invariants[]? | select(.invariant_type == "DESIRED_LIVE_STATE_CONSISTENCY") | .status] == ["INCONCLUSIVE"])'
assert_result "invariant provenance explains insufficiency" 'any(.invariants[]?; .invariant_type == "DESIRED_LIVE_STATE_CONSISTENCY" and .status == "INCONCLUSIVE" and (.reason | contains("baseline evidence is not substituted")))'
assert_result "evidence assessment identifies only Argo unavailable" '([.evidence_assessment.dimensions[] | select(.requirement_status == "REQUIRED" and .availability == "UNAVAILABLE") | .evidence_type] == ["argo_state"]) and any(.evidence_assessment.dimensions[]; .evidence_type == "argo_state" and .applicability == "APPLICABLE" and .source_provider == "argocd" and (.reason | contains("argo_state_unavailable_after_remediation")))'
assert_result "platform evidence validity" '.validity == "invalid_platform"'
assert_result "explicit verifier evidence reason" '.reason == "Required verification evidence was unavailable: argo_state."'
assert_result "final result INCONCLUSIVE" '.overall_status == "INCONCLUSIVE"'
assert_result "not FAILED or VERIFIED" '.overall_status != "FAILED" and .overall_status != "VERIFIED"'
assert_result "no unsupported forward risk inference" '(.forward_risks | length) == 0'

run_namespace="$(jq -er '.baseline_snapshot.observation.workloads[0].namespace | select(type == "string" and length > 0)' "$result")" || fail_assertion "cleanup namespace extracted"
if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
  deletion_timestamp="$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")"
  [[ -n "$deletion_timestamp" ]] || fail_assertion "cleanup deletion requested"
  kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/inconclusive-product-cleanup-wait.txt" 2>&1 || fail_assertion "cleanup deletion completed"
fi
remaining="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" || fail_assertion "cleanup namespace absence readable"
[[ -z "$remaining" ]] || fail_assertion "cleanup namespace absent"

jq '{label:"INCONCLUSIVE VERIFIER EVIDENCE CONTROL", agent_remediation:"SUCCESS", outcome:.outcome.status, validity, evidence_errors:.final_snapshot.gitops.errors, missing_argo_state:(.final_snapshot.gitops.argo_state == null), invariants, reason, overall_status}' "$result" > "$ARTIFACT_DIR/verification-proof-summary-inconclusive.json"
printf 'inconclusive_control=INCONCLUSIVE\n' >> "$ARTIFACT_DIR/cleanup.txt"
