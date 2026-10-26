#!/usr/bin/env bash
# External-agent VERIFIED control: a genuinely separate agent deployment
# remediates through authoritative Git desired state. Mirrors the positive
# control's reliability patterns, plus independence assertions.
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
namespace=evalplane-e2e-harness
agent_app=external-agent
pod=qualification-external
log="$ARTIFACT_DIR/qualification-external.log"
result="$ARTIFACT_DIR/verification-result-external.json"

collect_diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/qualification-external-pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/qualification-external-pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl -n argocd get application checkout-gitops-proof-external -o yaml > "$ARTIFACT_DIR/qualification-external-argo-application.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" get deployment "$agent_app" -o yaml > "$ARTIFACT_DIR/external-agent-deployment.yaml" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/qualification-external-all-pods-wide.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

fail_assertion() {
  echo "EXTERNAL PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/external-proof-assertion-failure.txt" || true
  if [[ -s "$result" ]] && jq -e 'type == "object"' "$result" >/dev/null 2>&1; then
    jq '{validity, overall_status, evidence_assessment, action_inspection, infrastructure_diff, outcome, side_effects, gitops: .final_snapshot.gitops, live_workloads: .final_snapshot.observation.workloads, invariants, stability, forward_risks}' "$result" >&2 || true
  fi
  collect_diagnostics
  exit 1
}

assert_result() {
  local name="$1"
  local expression="$2"
  echo "external proof assertion: $name"
  jq -e "$expression" "$result" >/dev/null || fail_assertion "$name"
}

assert_event() {
  local name="$1"
  local expression="$2"
  echo "external proof assertion: $name"
  jq -Re "$expression" "$log" >/dev/null || fail_assertion "$name"
}

timeout 30s kubectl apply -f backend/e2e/external-agent.yaml || fail_assertion "external agent applied"
if ! kubectl wait -n "$namespace" --for=condition=Available "deployment/$agent_app" --timeout=180s; then
  fail_assertion "external agent available"
fi
timeout 30s kubectl apply -f backend/e2e/qualification-external.yaml || fail_assertion "qualification pod applied"
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

assert_result "verification contracts" '.verifier_contract_version == "infrastructure-verifier:v3" and .snapshot_contract_version == "infrastructure-snapshot:v2" and .evidence_assessment.contract_version == "evidence-requirements:v1"'
assert_result "required evidence complete" '.evidence_assessment.requirements == {"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"REQUIRED","argo_state":"REQUIRED","stability":"REQUIRED"} and (.evidence_assessment.dimensions | length) == 5 and all(.evidence_assessment.dimensions[]; .requirement_status == "REQUIRED" and .applicability == "APPLICABLE" and .availability == "AVAILABLE")'
assert_result "evidence validity valid" '.validity == "valid"'
assert_result "overall status VERIFIED" '.overall_status == "VERIFIED"'
assert_result "external remediation authorized and executed" 'any(.action_inspection.actions[]?; .action_type == "set_desired_workload_image" and .authorized == true and .status == "executed")'
assert_result "action semantic is desired-state" 'all(.action_inspection.actions[]?; .action_type != "set_live_workload_image" and .action_type != "set_workload_image")'
assert_result "expected image diff v18 to v17" 'any(.infrastructure_diff[]?; .classification == "EXPECTED" and any(.fields[]?; .field == "image" and .before == "evalplane-reference:v18" and .after == "evalplane-reference:v17"))'
assert_result "immediate outcome PASSED" '.outcome.status == "PASSED"'
assert_result "side effects NONE" '.side_effects.status == "NONE"'
assert_result "Git desired image v17" '.final_snapshot.gitops.desired_state.image == "evalplane-reference:v17"'
assert_result "live Kubernetes image v17" 'any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v17")'
assert_result "Argo Synced" '.final_snapshot.gitops.argo_state.sync_status == "Synced"'
assert_result "Argo observed authoritative revision" '.final_snapshot.gitops.argo_state.observed_revision == .final_snapshot.gitops.desired_state.revision'
assert_result "desired/live invariant PASSED" 'any(.invariants[]?; .invariant_type == "DESIRED_LIVE_STATE_CONSISTENCY" and .status == "PASSED")'
assert_result "desired-state attribution succeeds" '.action_inspection.attribution.required == true and .action_inspection.attribution.status == "ATTRIBUTED" and (.action_inspection.attribution.actions | length) == 1 and .action_inspection.attribution.actions[0].status == "ATTRIBUTED"'
assert_result "stability STABLE" '.stability.status == "STABLE" and .stability.observations > 1'
assert_result "no forward risks" '(.forward_risks | length) == 0'
assert_event "gateway boundary" 'fromjson? | select(.kind == "gateway_boundary" and .value.checked == true and .value.requests > 0)'
assert_event "action proposed persisted" 'fromjson? | select(.kind == "persisted_action_proposed")'
assert_event "action transition persisted" 'fromjson? | select(.kind == "persisted_action_transition")'

# Independence: platform-observed digest pins the persisted implementation identity.
assert_event "external agent identity observed" 'fromjson? | select(.kind == "external_agent_identity" and (.value.observed_digest | test("^sha256:[0-9a-f]{64}$")) and .value.observed_digest == .value.pinned_identifier and .value.observer == "qualification-harness")'
assert_result "implementation identity pinned to observed digest" '.provenance.agent_implementation.contract_version == "agent-implementation-identity:v1" and (.provenance.agent_implementation.identifier | test("^sha256:[0-9a-f]{64}$"))'
if ! observed="$(jq -Rrc 'fromjson? | select(.kind == "external_agent_identity") | .value.observed_digest' "$log" | tail -1)"; then
  fail_assertion "observed digest extracted"
fi
[[ "$observed" =~ ^sha256:[0-9a-f]{64}$ ]] || fail_assertion "observed digest well-formed"
if ! pinned="$(jq -er '.provenance.agent_implementation.identifier' "$result")"; then
  fail_assertion "pinned identifier extracted"
fi
[[ "$observed" == "$pinned" ]] || fail_assertion "observed digest pins persisted identity"

# Independence: the external agent runtime holds no prohibited credentials.
agent_pod="$(timeout 30s kubectl -n "$namespace" get pod -l "app=$agent_app" -o jsonpath='{.items[?(@.status.phase=="Running")].metadata.name}' | tr ' ' '\n' | head -1)"
[[ -n "$agent_pod" ]] || fail_assertion "external agent pod located"
timeout 30s kubectl -n "$namespace" get pod "$agent_pod" -o json > "$ARTIFACT_DIR/external-agent-pod.json" || fail_assertion "external agent pod readable"
if jq -e '.spec.containers[] | select(.envFrom != null or ([.env[]? | select(.valueFrom != null)] | length) > 0 or ([.volumeMounts[]? | select(.name == "kube-api-access-" or (.name | test("secret|credential"; "i")))] | length) > 0)' "$ARTIFACT_DIR/external-agent-pod.json" >/dev/null; then
  fail_assertion "external agent pod references secrets"
fi
if jq -er '.spec.containers[].env[]?.name' "$ARTIFACT_DIR/external-agent-pod.json" 2>/dev/null | grep -Ei 'token|password|secret|credential|kubeconfig|private_key|api_key' >/dev/null; then
  fail_assertion "external agent env carries credential material"
fi
if [[ -n "$(jq -r '.spec.serviceAccountName // empty' "$ARTIFACT_DIR/external-agent-pod.json")" ]] && [[ "$(jq -r '.spec.serviceAccountName' "$ARTIFACT_DIR/external-agent-pod.json")" != "default" ]]; then
  fail_assertion "external agent runs under a privileged service account"
fi

run_namespace="$(jq -er '.baseline_snapshot.observation.workloads[0].namespace | select(type == "string" and length > 0)' "$result")" || fail_assertion "cleanup namespace extracted"
if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
  deletion_timestamp="$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")"
  [[ -n "$deletion_timestamp" ]] || fail_assertion "cleanup deletion requested"
  kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/external-product-cleanup-wait.txt" 2>&1 || fail_assertion "cleanup deletion completed"
fi
remaining="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" || fail_assertion "cleanup namespace absence readable"
[[ -z "$remaining" ]] || fail_assertion "cleanup namespace absent"

jq '{label:"EXTERNAL AGENT DURABLE GITOPS CONTROL", agent_remediation:"SUCCESS", immediate_outcome:.outcome.status, side_effects:.side_effects.status, git_desired_image:.final_snapshot.gitops.desired_state.image, live_images:[.final_snapshot.observation.workloads[]? | select(.name == "eval-reference-app") | .image], argo_sync:.final_snapshot.gitops.argo_state.sync_status, invariants, stability, forward_risks, overall_status}' "$result" > "$ARTIFACT_DIR/verification-proof-summary-external.json"
printf 'external_control=VERIFIED\n' >> "$ARTIFACT_DIR/cleanup.txt"
