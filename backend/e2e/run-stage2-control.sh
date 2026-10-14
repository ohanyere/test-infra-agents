#!/usr/bin/env bash
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
mode="${1:?usage: run-stage2-control.sh step-limit|temporal|insufficient}"
namespace=evalplane-e2e-harness
case "$mode" in
  step-limit) pod=qualification-stage2-step-limit; manifest=backend/e2e/qualification-stage2-step-limit.yaml ;;
  temporal) pod=qualification-stage2-temporal; manifest=backend/e2e/qualification-stage2-temporal.yaml ;;
  insufficient) pod=qualification-stage2-insufficient; manifest=backend/e2e/qualification-stage2-insufficient.yaml ;;
  *) echo "unknown Stage 2 control: $mode" >&2; exit 2 ;;
esac
log="$ARTIFACT_DIR/$pod.log"
verification="$ARTIFACT_DIR/$pod-verification.json"
evaluation="$ARTIFACT_DIR/$pod-evaluation.json"
input="$ARTIFACT_DIR/$pod-input.json"

diagnostics() {
  timeout 30s kubectl -n "$namespace" get pod "$pod" -o yaml > "$ARTIFACT_DIR/$pod.yaml" 2>&1 || true
  timeout 30s kubectl -n "$namespace" describe pod "$pod" > "$ARTIFACT_DIR/$pod-describe.txt" 2>&1 || true
  timeout 30s kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
  timeout 30s kubectl get pods -A -o wide > "$ARTIFACT_DIR/$pod-all-pods.txt" 2>&1 || true
  jq -Rrc 'fromjson? | select(.kind == "qualification_stage" or .kind == "qualification_failure" or .kind == "worker_failure" or .kind == "persistence_failure")' "$log" >&2 || true
}

proof_diagnostics() {
  if [[ -s "$verification" ]]; then
    echo "Persisted verification summary:" >&2
    jq -c '{overall_status, validity, reason, outcome: .outcome.status, evidence_assessment: {contract_version: .evidence_assessment.contract_version, requirements: .evidence_assessment.requirements, dimensions: [.evidence_assessment.dimensions[]? | {evidence_type, requirement_status, availability, reason, source_provider, evidence_references}]}, attribution: .action_inspection.attribution.status}' "$verification" >&2 || true
  fi
  if [[ -s "$evaluation" ]]; then
    echo "Persisted recovery criterion summary:" >&2
    jq -c '{status, criteria: [.criteria[]? | {key, name, metric_key, evidence: {status: .evidence.status, reason: .evidence.reason, observed_state: .evidence.observed_state, observed_value: .evidence.observed_value, evidence_ids: .evidence.evidence_ids, evidence_selection: .evidence.evidence_selection}}]}' "$evaluation" >&2 || true
  fi
  if [[ -s "$input" ]]; then
    echo "Persisted action and observation timeline:" >&2
    jq -c '{actions: [.Actions[]? | {id, action_type, status, authorized, started_at, completed_at, created_at}], events: [.Events[]? | select(.event_type == "observation" or .event_type == "telemetry" or .event_type == "action" or .event_type == "decision") | {id, sequence, event_type, occurred_at, summary, payload}]}' "$input" >&2 || true
  fi
}

fail() {
  echo "STAGE 2 ${mode^^} PROOF ASSERTION FAILED: $1" >&2
  printf '%s\n' "$1" > "$ARTIFACT_DIR/$pod-assertion-failure.txt" || true
  proof_diagnostics
  diagnostics
  exit 1
}

assert_json() {
  local file="$1" name="$2" expression="$3"
  echo "Stage 2 $mode proof assertion: $name"
  if ! jq -e "$expression" "$file" >/dev/null; then
    echo "Failed JSON assertion in $(basename "$file"): $expression" >&2
    fail "$name"
  fi
}

timeout 30s kubectl apply -f "$manifest" || fail "qualification pod applied"
if ! kubectl wait -n "$namespace" --for=condition=Ready "pod/$pod" --timeout=180s; then
  fail "qualification pod ready"
fi

deadline=$((SECONDS + 720))
last_phase=""
succeeded=false
while ((SECONDS < deadline)); do
  phase="$(timeout 30s kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.status.phase}')" || fail "qualification phase readable"
  if [[ "$phase" != "$last_phase" ]]; then
    echo "Stage 2 $mode qualification phase: ${phase:-empty}"
    last_phase="$phase"
  fi
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
jq -Rc 'fromjson? | select(.kind == "persisted_evaluation") | .value' "$log" | tail -1 > "$evaluation"
jq -Rc 'fromjson? | select(.kind == "persisted_input") | .value' "$log" | tail -1 > "$input"
for proof in "$verification" "$evaluation" "$input"; do
  [[ -s "$proof" ]] && jq -e 'type == "object"' "$proof" >/dev/null || fail "persisted proof object $(basename "$proof")"
done

assert_json "$verification" "Stage 1 contracts unchanged" '.verifier_contract_version == "infrastructure-verifier:v3" and .evidence_assessment.contract_version == "evidence-requirements:v1"'
case "$mode" in
  step-limit)
    assert_json "$verification" "step exhaustion is INCONCLUSIVE" '.overall_status == "INCONCLUSIVE"'
    assert_json "$input" "terminal run failed" '.Run.status == "failed"'
    assert_json "$input" "machine-readable step-limit reason persisted" 'any(.Events[]?; .event_type == "system" and .summary == "agent_step_limit_reached" and .payload.classification == "agent")'
    jq -Re 'fromjson? | select(.kind == "expected_worker_terminal" and .value.reason == "agent_step_limit_reached")' "$log" >/dev/null || fail "expected step-limit terminal observed"
    ;;
  temporal)
    assert_json "$verification" "temporal recovery VERIFIED" '.overall_status == "VERIFIED" and .outcome.status == "PASSED"'
    assert_json "$evaluation" "both recovery criteria passed" '(.criteria | length) == 2 and all(.criteria[]; .evidence.status == "passed" and (.evidence.evidence_selection | contains("latest executed mutating action")))'
    assert_json "$input" "real mutation executed" 'any(.Actions[]?; .action_type == "set_desired_workload_image" and .status == "executed" and .authorized == true)'
    ;;
  insufficient)
    assert_json "$verification" "incomplete recovery evidence INCONCLUSIVE" '.overall_status == "INCONCLUSIVE"'
    assert_json "$evaluation" "recovery metric unavailable after action" 'any(.criteria[]?; .evidence.status == "inconclusive" and (.evidence.reason | contains("no eligible post-action observation contained this metric")))'
    assert_json "$input" "real mutation executed" 'any(.Actions[]?; .action_type == "set_desired_workload_image" and .status == "executed" and .authorized == true)'
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

printf 'stage2_%s=VERIFIED\n' "$mode" >> "$ARTIFACT_DIR/cleanup.txt"
