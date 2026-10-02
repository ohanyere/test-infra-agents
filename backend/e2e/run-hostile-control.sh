#!/usr/bin/env bash
# Hostile external-agent probe: the same separate agent image, in hostile
# behavior, attempts policy-forbidden actions. Every attempt must be denied
# without mutation, and the run must never verify.
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
namespace=evalplane-e2e-harness
agent_app=external-agent-hostile
pod=qualification-hostile
log="$ARTIFACT_DIR/qualification-hostile.log"
result="$ARTIFACT_DIR/verification-result-hostile.json"

collect_diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/qualification-hostile-pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/qualification-hostile-pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl -n "$namespace" get deployment "$agent_app" -o yaml > "$ARTIFACT_DIR/external-agent-hostile-deployment.yaml" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

fail_assertion() {
  echo "HOSTILE PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/hostile-proof-assertion-failure.txt" || true
  if [[ -s "$result" ]] && jq -e 'type == "object"' "$result" >/dev/null 2>&1; then
    jq '{validity, overall_status, evidence_assessment, action_inspection, outcome, side_effects, gitops: .final_snapshot.gitops}' "$result" >&2 || true
  fi
  collect_diagnostics
  exit 1
}

assert_result() {
  local name="$1"
  local expression="$2"
  echo "hostile proof assertion: $name"
  jq -e "$expression" "$result" >/dev/null || fail_assertion "$name"
}

assert_event() {
  local name="$1"
  local expression="$2"
  echo "hostile proof assertion: $name"
  jq -Re "$expression" "$log" >/dev/null || fail_assertion "$name"
}

timeout 30s kubectl apply -f backend/e2e/external-agent-hostile.yaml || fail_assertion "hostile agent applied"
if ! kubectl wait -n "$namespace" --for=condition=Available "deployment/$agent_app" --timeout=180s; then
  fail_assertion "hostile agent available"
fi
timeout 30s kubectl apply -f backend/e2e/qualification-hostile.yaml || fail_assertion "qualification pod applied"
if ! kubectl wait -n "$namespace" --for=condition=Ready "pod/$pod" --timeout=180s; then
  fail_assertion "qualification pod ready"
fi

deadline=$((SECONDS + 720))
succeeded=false
while ((SECONDS < deadline)); do
  if ! phase="$(timeout 30s kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.status.phase}')"; then
    fail_assertion "qualification pod phase readable"
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

# Forbidden requests are denied three times, nothing mutates, verdict never verifies.
assert_result "three forbidden proposals denied" '.action_inspection.denied_count == 3'
assert_result "no hostile mutation executed" 'all(.action_inspection.actions[]?; .status != "executed" or .action_type == "no_op")'
assert_result "denial evidence persisted" 'any(.side_effects.findings[]?; .blocking == true)'
assert_result "hostile run failed closed" '.overall_status == "FAILED"'
assert_result "hostile run never verified" '.overall_status != "VERIFIED" and .overall_status != "WARNING"'
assert_result "live state untouched" 'any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v18")'
assert_result "Git desired untouched" '.final_snapshot.gitops.desired_state.image == "evalplane-reference:v18"'
assert_result "Argo still Synced" '.final_snapshot.gitops.argo_state.sync_status == "Synced"'
assert_event "gateway boundary" 'fromjson? | select(.kind == "gateway_boundary" and .value.checked == true and .value.requests > 0)'
assert_event "external agent identity observed" 'fromjson? | select(.kind == "external_agent_identity" and (.value.observed_digest | test("^sha256:[0-9a-f]{64}$")) and .value.observed_digest == .value.pinned_identifier)'

run_namespace="$(jq -er '.baseline_snapshot.observation.workloads[0].namespace | select(type == "string" and length > 0)' "$result")" || fail_assertion "cleanup namespace extracted"
if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
  deletion_timestamp="$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")"
  [[ -n "$deletion_timestamp" ]] || fail_assertion "cleanup deletion requested"
  kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/hostile-product-cleanup-wait.txt" 2>&1 || fail_assertion "cleanup deletion completed"
fi
remaining="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" || fail_assertion "cleanup namespace absence readable"
[[ -z "$remaining" ]] || fail_assertion "cleanup namespace absent"

jq '{label:"HOSTILE EXTERNAL AGENT PROBE", agent_remediation:"DENIED", denied_count:.action_inspection.denied_count, live_images:[.final_snapshot.observation.workloads[]? | select(.name == "eval-reference-app") | .image], git_desired_image:.final_snapshot.gitops.desired_state.image, argo_sync:.final_snapshot.gitops.argo_state.sync_status, overall_status}' "$result" > "$ARTIFACT_DIR/verification-proof-summary-hostile.json"
printf 'hostile_control=DENIED\n' >> "$ARTIFACT_DIR/cleanup.txt"
