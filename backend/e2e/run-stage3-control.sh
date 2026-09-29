#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
mode="${1:?usage: run-stage3-control.sh attributed|contradiction}"
namespace=evalplane-e2e-harness
case "$mode" in
  attributed) pod=qualification-stage3-attributed; manifest=backend/e2e/qualification-stage3-attributed.yaml ;;
  contradiction) pod=qualification-stage3-contradiction; manifest=backend/e2e/qualification-stage3-contradiction.yaml ;;
  *) echo "unknown Stage 3 control: $mode" >&2; exit 2 ;;
esac
log="$ARTIFACT_DIR/$pod.log"
verification="$ARTIFACT_DIR/$pod-verification.json"
input="$ARTIFACT_DIR/$pod-input.json"

diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/$pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/$pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/$pod-all-pods.txt" 2>&1 || true
  timeout 30s kubectl -n argocd get applications -o wide > "$ARTIFACT_DIR/$pod-argo.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure")' "$log" >&2 || true
}

fail() {
  echo "STAGE 3 ${mode^^} PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/$pod-assertion-failure.txt" || true
  diagnostics
  exit 1
}

assert_json() {
  local name="$1" expression="$2"
  echo "Stage 3 $mode proof assertion: $name"
  jq -e "$expression" "$verification" >/dev/null || fail "$name"
}

timeout 30s kubectl apply -f "$manifest" || fail "qualification pod applied"
kubectl wait -n "$namespace" --for=condition=Ready "pod/$pod" --timeout=180s || fail "qualification pod ready"
deadline=$((SECONDS + 720))
succeeded=false
while ((SECONDS < deadline)); do
  phase="$(timeout 30s kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.status.phase}')" || fail "qualification phase readable"
  case "$phase" in
    Succeeded) succeeded=true; break ;;
    Failed|Unknown) fail "qualification terminal phase $phase" ;;
    Pending|Running|"") ;;
    *) fail "unexpected qualification phase $phase" ;;
  esac
  sleep 2
done
[[ "$succeeded" == true ]] || fail "qualification completed within 720 seconds"

timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" || fail "qualification log captured"
jq -Rc 'fromjson? | select(.kind == "persisted_verification") | .value' "$log" | tail -1 > "$verification"
jq -Rc 'fromjson? | select(.kind == "persisted_input") | .value' "$log" | tail -1 > "$input"
[[ -s "$verification" ]] && jq -e 'type == "object"' "$verification" >/dev/null || fail "persisted verification object"
[[ -s "$input" ]] && jq -e 'type == "object"' "$input" >/dev/null || fail "persisted input object"
assert_json "Stage 1 contracts unchanged" '.verifier_contract_version == "infrastructure-verifier:v3" and .evidence_assessment.contract_version == "evidence-requirements:v1"'
assert_json "attribution contract persisted" '.action_inspection.attribution.contract_version == "action-attribution:v1" and .action_inspection.attribution.required == true'

case "$mode" in
  attributed)
    assert_json "platform mutation ATTRIBUTED" '.action_inspection.attribution.status == "ATTRIBUTED" and (.action_inspection.attribution.actions | length) == 1 and .action_inspection.attribution.actions[0].status == "ATTRIBUTED"'
    assert_json "same resource identity and v18 to v17 transition" '.action_inspection.attribution.actions[0] as $a | $a.pre_mutation.uid != "" and $a.pre_mutation.uid == $a.post_mutation.uid and $a.post_mutation.uid == $a.later_observed.uid and $a.pre_mutation.value == "evalplane-reference:v18" and $a.requested_value == "evalplane-reference:v17" and $a.post_mutation.value == "evalplane-reference:v17" and $a.later_observed.value == "evalplane-reference:v17" and $a.post_mutation.generation > $a.pre_mutation.generation and $a.later_observed.generation == $a.post_mutation.generation'
    assert_json "canonical negative semantics preserved" '.overall_status == "FAILED" and .outcome.status == "PASSED" and any(.action_inspection.actions[]; .action_type == "set_workload_image" and .status == "executed") and .final_snapshot.gitops.desired_state.image == "evalplane-reference:v18" and any(.final_snapshot.observation.workloads[]; .image == "evalplane-reference:v17") and .final_snapshot.gitops.argo_state.sync_status == "OutOfSync" and any(.invariants[]; .status == "FAILED") and .stability.status == "UNSTABLE" and any(.forward_risks[]; .severity == "HIGH")'
    ;;
  contradiction)
    assert_json "plausible final value is NOT_ATTRIBUTED" '.action_inspection.attribution.status == "NOT_ATTRIBUTED" and .action_inspection.attribution.actions[0].status == "NOT_ATTRIBUTED" and .action_inspection.attribution.actions[0].post_mutation.value == "evalplane-reference:v17" and .action_inspection.attribution.actions[0].later_observed.value == "evalplane-reference:v17" and .action_inspection.attribution.actions[0].later_observed.generation > .action_inspection.attribution.actions[0].post_mutation.generation'
    assert_json "contradiction blocks VERIFIED" '.overall_status == "FAILED" and any(.side_effects.findings[]; .type == "action_attribution_failed" and .blocking == true)'
    ;;
esac

run_namespace="$(jq -er '.Run.environment_ref // empty | sub("^k8s://"; "") | select(length > 0)' "$input")" || true
if [[ -n "$run_namespace" ]]; then
  if namespace_json="$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o json)" && [[ -n "$namespace_json" ]]; then
    [[ -n "$(jq -r '.metadata.deletionTimestamp // empty' <<< "$namespace_json")" ]] || fail "cleanup deletion requested"
    timeout 150s kubectl wait --for=delete "namespace/$run_namespace" --timeout=120s > "$ARTIFACT_DIR/$pod-cleanup.txt" 2>&1 || fail "cleanup deletion completed"
  fi
  [[ -z "$(timeout 30s kubectl get namespace "$run_namespace" --ignore-not-found -o name)" ]] || fail "cleanup namespace absent"
fi

printf 'stage3_%s=VERIFIED\n' "$mode" >> "$ARTIFACT_DIR/cleanup.txt"
