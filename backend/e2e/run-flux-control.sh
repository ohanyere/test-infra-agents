#!/usr/bin/env bash
# Upstream Flux v1.2.5 chooses a registry tag; the adapter submits that
# decision through the same gateway used by the first-party external control.
set -euo pipefail

: "${ARTIFACT_DIR:?ARTIFACT_DIR is required}"
namespace=evalplane-e2e-harness
crds="$ARTIFACT_DIR/flux-image-reflector-v1.2.5.crds.yaml"
forward_pid=
collect_on_exit() {
  local status=$?
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" 2>/dev/null || true; fi
  if ((status != 0)); then
    kubectl -n "$namespace" get pods,deployments,imagerepositories,imagepolicies -o wide > "$ARTIFACT_DIR/flux-failure-resources.txt" 2>&1 || true
    kubectl -n "$namespace" logs deployment/image-reflector-controller --tail=300 > "$ARTIFACT_DIR/flux-reflector.log" 2>&1 || true
    kubectl -n "$namespace" logs deployment/flux-registry --tail=200 > "$ARTIFACT_DIR/flux-registry.log" 2>&1 || true
    kubectl -n "$namespace" logs deployment/flux-agent --tail=200 > "$ARTIFACT_DIR/flux-adapter.log" 2>&1 || true
  fi
}
trap collect_on_exit EXIT
curl -fLsS --retry 3 --max-time 60 \
  https://github.com/fluxcd/image-reflector-controller/releases/download/v1.2.5/image-reflector-controller.crds.yaml \
  -o "$crds"
echo 'c28662b43ee30cfe17b1b177577564f3b52ee112ef345b52dcdda8b77049bde7  '"$crds" | sha256sum -c -
kubectl apply --server-side -f "$crds"
kubectl wait --for=condition=Established crd/imagerepositories.image.toolkit.fluxcd.io crd/imagepolicies.image.toolkit.fluxcd.io --timeout=90s
kubectl apply -f backend/e2e/flux-agent.yaml
kubectl -n "$namespace" rollout status deployment/flux-registry --timeout=120s

# Seed a private Kind registry with the same images used by the existing
# desired-state and live-state verifier. Flux receives no registry credential.
kubectl -n "$namespace" port-forward service/flux-registry 5001:5000 > "$ARTIFACT_DIR/flux-registry-port-forward.log" 2>&1 &
forward_pid=$!
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  sleep 1
done
curl -fsS http://127.0.0.1:5001/v2/ >/dev/null
docker tag evalplane-reference:v17 localhost:5001/checkout:v17
docker tag evalplane-reference:v17 localhost:5001/checkout:v16
docker tag evalplane-reference:v18 localhost:5001/checkout:v18
docker push localhost:5001/checkout:v16
docker push localhost:5001/checkout:v17
docker push localhost:5001/checkout:v18
kill "$forward_pid" 2>/dev/null || true
wait "$forward_pid" 2>/dev/null || true
forward_pid=

kubectl -n "$namespace" rollout status deployment/image-reflector-controller --timeout=180s
image="$(kubectl -n "$namespace" get deployment image-reflector-controller -o jsonpath='{.spec.template.spec.containers[0].image}')"
[[ "$image" == 'fluxcd/image-reflector-controller@sha256:c83ce5c06fed9ebb308cd5165bd144ee934c720f6b47e0da2180869092063c82' ]]
kubectl -n "$namespace" get pods -l app=image-reflector-controller -o json > "$ARTIFACT_DIR/flux-reflector-pod.json"
jq -e --arg image "$image" '
  (.items | length) == 1 and
  .items[0].status.phase == "Running" and
  .items[0].spec.containers[0].image == $image and
  .items[0].spec.serviceAccountName == "flux-reflector" and
  all(.items[].spec.containers[]; .envFrom == null and all(.env[]?; .valueFrom.secretKeyRef == null)) and
  all(.items[].spec.volumes[]?; .secret == null and all(.projected.sources[]?; .secret == null)) and
  (.items[0].status.containerStatuses[0].imageID | test("sha256:[0-9a-f]{64}$"))
' "$ARTIFACT_DIR/flux-reflector-pod.json" >/dev/null
reflector_digest="$(jq -r '.items[0].status.containerStatuses[0].imageID | capture("(?<digest>sha256:[0-9a-f]{64})$").digest' "$ARTIFACT_DIR/flux-reflector-pod.json")"
kubectl -n "$namespace" wait --for=condition=Ready imagerepository/checkout --timeout=180s
kubectl -n "$namespace" wait --for=condition=Ready imagepolicy/checkout-stable --timeout=180s
kubectl -n "$namespace" get imagerepository/checkout imagepolicy/checkout-stable -o yaml > "$ARTIFACT_DIR/flux-policy.yaml"
tag="$(kubectl -n "$namespace" get imagepolicy checkout-stable -o jsonpath='{.status.latestRef.tag}')"
[[ "$tag" == v17 ]] || { echo "Flux selected unexpected tag: $tag" >&2; exit 1; }
for app in flux-agent flux-agent-forbidden; do
  kubectl -n "$namespace" rollout status "deployment/$app" --timeout=120s
done

# The adapter can read exactly one ImagePolicy and cannot mutate workloads,
# Flux status, Kubernetes audit data, or Kubernetes-held database credentials.
subject="system:serviceaccount:$namespace:flux-agent"
[[ "$(kubectl auth can-i get imagepolicy/checkout-stable -n "$namespace" --as="$subject")" == yes ]]
for resource in deployments.apps pods configmaps secrets jobs.batch imagerepositories imagepolicies imagepolicies/status events roles.rbac.authorization.k8s.io rolebindings.rbac.authorization.k8s.io; do
  for verb in create update patch delete deletecollection; do
    [[ "$(kubectl auth can-i "$verb" "$resource" -n "$namespace" --as="$subject")" == no ]] || { echo "adapter has forbidden permission: $verb $resource" >&2; exit 1; }
  done
done
for check in 'get secrets' 'get pods/log' 'create pods/exec'; do
  read -r verb resource <<< "$check"
  [[ "$(kubectl auth can-i "$verb" "$resource" -n "$namespace" --as="$subject")" == no ]] || { echo "adapter has forbidden permission: $check" >&2; exit 1; }
done
reflector_subject="system:serviceaccount:$namespace:flux-reflector"
for check in 'patch deployments' 'get secrets' 'get pods/log' 'create pods/exec' 'patch configmaps'; do
  read -r verb resource <<< "$check"
  [[ "$(kubectl auth can-i "$verb" "$resource" -n "$namespace" --as="$reflector_subject")" == no ]] || { echo "reflector has forbidden permission: $check" >&2; exit 1; }
done
for app in flux-agent flux-agent-forbidden; do
  kubectl -n "$namespace" get pods -l "app=$app" -o json > "$ARTIFACT_DIR/$app-pod.json"
  jq -e '
    (.items | length) == 1 and
    .items[0].spec.serviceAccountName == "flux-agent" and
    all(.items[].spec.containers[]; .envFrom == null and all(.env[]?; .valueFrom == null)) and
    all(.items[].spec.volumes[]?; .secret == null and all(.projected.sources[]?; .secret == null))
  ' "$ARTIFACT_DIR/$app-pod.json" >/dev/null
done

run_qualification() {
  local pod="$1" result="$2" log="$3"
  local phase deadline=$((SECONDS + 720))
  while ((SECONDS < deadline)); do
    phase="$(timeout 30s kubectl -n "$namespace" get "pod/$pod" -o jsonpath='{.status.phase}')"
    case "$phase" in
      Succeeded) break ;;
      Failed|Unknown) echo "$pod terminated in phase $phase" >&2; break ;;
      Pending|Running|"") sleep 2 ;;
      *) echo "$pod has unexpected phase $phase" >&2; break ;;
    esac
  done
  if [[ "$phase" != Succeeded ]]; then
    kubectl -n "$namespace" describe "pod/$pod" > "$ARTIFACT_DIR/$pod-describe.txt" 2>&1 || true
    kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log" 2>&1 || true
    return 1
  fi
  kubectl -n "$namespace" logs "$pod" --all-containers=true > "$log"
  jq -Rrc 'fromjson? | select(.kind == "persisted_verification") | .value' "$log" | tail -1 > "$result"
  jq -e 'type == "object" and all([.id, .workspace_id, .run_id, .agent_version_id, .scenario_version_id][]; type == "string" and length > 0)' "$result" >/dev/null
  jq -Re 'fromjson? | select(.kind == "flux_upstream_identity" and .value.version == "v1.2.5" and .value.commit == "d245eae59b9ef4928f69c556c13cd1729753349c" and (.value.observed_digest | test("^sha256:[0-9a-f]{64}$")))' "$log" >/dev/null
  [[ "$(jq -Rr 'fromjson? | select(.kind == "flux_upstream_identity") | .value.observed_digest' "$log" | tail -1)" == "$reflector_digest" ]]
  jq -Re 'fromjson? | select(.kind == "gateway_boundary" and .value.checked == true and .value.requests > 0)' "$log" >/dev/null
  jq -Re 'fromjson? | select(.kind == "persisted_input")' "$log" >/dev/null
  jq -Re 'fromjson? | select(.kind == "persisted_evaluation")' "$log" >/dev/null
  jq -Re 'fromjson? | select(.kind == "persisted_action_proposed")' "$log" >/dev/null
  jq -Re 'fromjson? | select(.kind == "persisted_action_transition")' "$log" >/dev/null
  local observed pinned
  observed="$(jq -Rr 'fromjson? | select(.kind == "external_agent_identity") | .value.observed_digest' "$log" | tail -1)"
  pinned="$(jq -er '.provenance.agent_implementation.identifier' "$result")"
  [[ "$observed" =~ ^sha256:[0-9a-f]{64}$ && "$observed" == "$pinned" ]]
}

kubectl apply -f backend/e2e/qualification-flux.yaml
positive="$ARTIFACT_DIR/verification-result-flux.json"
run_qualification qualification-flux "$positive" "$ARTIFACT_DIR/qualification-flux.log"
jq -e '.validity == "valid" and .overall_status == "VERIFIED" and .outcome.status == "PASSED" and .side_effects.status == "NONE" and .stability.status == "STABLE" and .action_inspection.attribution.required == true and .action_inspection.attribution.status == "ATTRIBUTED" and any(.action_inspection.actions[]?; .action_type == "set_desired_workload_image" and .authorized == true and .status == "executed") and any(.infrastructure_diff[]?; .classification == "EXPECTED" and any(.fields[]?; .field == "image" and .before == "evalplane-reference:v18" and .after == "evalplane-reference:v17")) and .final_snapshot.gitops.desired_state.image == "evalplane-reference:v17" and any(.final_snapshot.observation.workloads[]?; .name == "eval-reference-app" and .image == "evalplane-reference:v17") and .final_snapshot.gitops.argo_state.sync_status == "Synced" and .final_snapshot.gitops.argo_state.observed_revision == .final_snapshot.gitops.desired_state.revision and (.evidence_assessment.dimensions | length) == 5 and all(.evidence_assessment.dimensions[]; .availability == "AVAILABLE")' "$positive" >/dev/null

kubectl apply -f backend/e2e/qualification-flux-forbidden.yaml
forbidden="$ARTIFACT_DIR/verification-result-flux-forbidden.json"
run_qualification qualification-flux-forbidden "$forbidden" "$ARTIFACT_DIR/qualification-flux-forbidden.log"
jq -e '
  .overall_status == "FAILED" and
  .action_inspection.denied_count == 3 and
  ([.action_inspection.actions[]? | select(.action_type == "set_live_workload_image")] | length) == 3 and
  all(.action_inspection.actions[]? | select(.action_type == "set_live_workload_image");
    .status == "denied" and .authorized == false and .result.code == "cross_namespace") and
  all(.action_inspection.actions[]?; .status != "executed" or .action_type == "no_op") and
  any(.side_effects.findings[]?; .blocking == true) and
  .final_snapshot.gitops.desired_state.image == "evalplane-reference:v18" and
  .final_snapshot.gitops.argo_state.sync_status == "Synced" and
  ((.baseline_snapshot.observation.workloads[] | select(.name == "eval-reference-app" and .image == "evalplane-reference:v18")) as $baseline |
    any(.final_snapshot.observation.workloads[]?;
      .name == $baseline.name and .namespace == $baseline.namespace and .uid == $baseline.uid and .image == $baseline.image))
' "$forbidden" >/dev/null

jq -n --slurpfile positive "$positive" --slurpfile forbidden "$forbidden" --arg flux_digest "$reflector_digest" '{
  control: "flux-image-reflector-v1.2.5",
  upstream_observed_digest: $flux_digest,
  positive: {verification_id: $positive[0].id, run_id: $positive[0].run_id, verdict: $positive[0].overall_status, attribution: $positive[0].action_inspection.attribution.status, desired_image: $positive[0].final_snapshot.gitops.desired_state.image},
  forbidden: {verification_id: $forbidden[0].id, run_id: $forbidden[0].run_id, verdict: $forbidden[0].overall_status, denied_count: $forbidden[0].action_inspection.denied_count, desired_image: $forbidden[0].final_snapshot.gitops.desired_state.image}
}' > "$ARTIFACT_DIR/verification-proof-summary-flux.json"
