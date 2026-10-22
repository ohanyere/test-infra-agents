#!/usr/bin/env bash
set -euo pipefail

: "${OPERATION_PROOF_PASSWORD:?proof password required}"
: "${OPERATION_PROOF_FILE:?public proof fixture required}"
: "${OPERATION_PROOF_API_URL:?local API URL required}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"
api="${OPERATION_PROOF_API_URL%/}"
workspace="$(jq -er .workspace_id "$OPERATION_PROOF_FILE")"
other_workspace="$(jq -er .other_workspace_id "$OPERATION_PROOF_FILE")"
environment="$(jq -er .environment_id "$OPERATION_PROOF_FILE")"
session="$(jq -er .session_id "$OPERATION_PROOF_FILE")"
email="$(jq -er .email "$OPERATION_PROOF_FILE")"
other_email="$(jq -er .other_email "$OPERATION_PROOF_FILE")"
base="$api/v1/workspaces/$workspace/customer-kubernetes-environments/$environment/observation-sessions/$session"

jq -nc --arg email "$email" --arg password "$OPERATION_PROOF_PASSWORD" '{email:$email,password:$password}' |
  curl --fail --silent --show-error --cookie-jar "$work/owner.cookies" -H 'Content-Type: application/json' --data-binary @- "$api/v1/auth/login" >/dev/null
curl --fail --silent --show-error --cookie "$work/owner.cookies" "$base/operational-assessments" > "$work/reports.json"
jq -e --slurpfile expected "$OPERATION_PROOF_FILE" '
  .items | length == 1 and
  .[0].canonical_overall_status == "INCONCLUSIVE" and
  (.[0].observed_actions | any(.[]; .id == $expected[0].audit_id and .operation == "patch" and .kind == "Deployment" and .namespace == $expected[0].namespace and .name == $expected[0].resource_name and .origin_authenticated == false)) and
  (.[0].operation_explanations | any(.[];
    .key == ("audit:" + $expected[0].audit_id) and
    .requested == "No authenticated requested action linked" and
    .attempted == false and
    .resource == ($expected[0].namespace + "/" + $expected[0].resource_name) and
    .gateway_decision == "not linked" and
    .attribution_status == "not established" and
    .outcome_status == "not established" and
    (.observation_status | contains("origin unverified")) and
    (.evidence | any(.reference == $expected[0].evidence_reference)) and
    (.rule_results | any(.[]; .rule_id == $expected[0].rule_id and .code == "UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE" and .status == "INCONCLUSIVE"))
  )) and
  (.[0].findings | any(.[]; .rule_id == $expected[0].rule_id and .code == "UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE" and .affected_resource == ($expected[0].namespace + "/" + $expected[0].resource_name) and (.evidence | any(.[]; .reference == $expected[0].audit_id))))
' "$work/reports.json" >/dev/null

reference="$(jq -er .evidence_reference "$OPERATION_PROOF_FILE")"
curl --fail --silent --show-error --get --data-urlencode "reference=$reference" --cookie "$work/owner.cookies" "$base/evidence-detail" > "$work/evidence.json"
jq -e --arg reference "$reference" --arg digest "$(jq -er .source_event_digest "$OPERATION_PROOF_FILE")" '.reference == $reference and .kind == "audit" and .digest == $digest and (.summary | contains("origin unverified"))' "$work/evidence.json" >/dev/null

jq -nc --arg email "$other_email" --arg password "$OPERATION_PROOF_PASSWORD" '{email:$email,password:$password}' |
  curl --fail --silent --show-error --cookie-jar "$work/other.cookies" -H 'Content-Type: application/json' --data-binary @- "$api/v1/auth/login" >/dev/null
for url in "$base/operational-assessments" "$base/evidence-detail?reference=$reference"; do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' --cookie "$work/other.cookies" "$url")"
  test "$status" = 403
done
status="$(curl --silent --output /dev/null --write-out '%{http_code}' --cookie "$work/owner.cookies" "$api/v1/workspaces/$other_workspace/customer-kubernetes-environments/$environment/observation-sessions/$session/operational-assessments")"
test "$status" = 403
if [[ -n "${OPERATION_PROOF_ARTIFACT_DIR:-}" ]]; then
  mkdir -p "$OPERATION_PROOF_ARTIFACT_DIR"
  cp "$work/reports.json" "$OPERATION_PROOF_ARTIFACT_DIR/operation-reports-api.json"
  cp "$work/evidence.json" "$OPERATION_PROOF_ARTIFACT_DIR/operation-evidence-api.json"
  printf 'owner_report=200\nowner_evidence=200\nother_tenant_report=403\nother_tenant_evidence=403\n' > "$OPERATION_PROOF_ARTIFACT_DIR/operation-scope-checks.txt"
fi
echo 'Tenant-scoped operation explanation API: PASS'
