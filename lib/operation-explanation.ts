import type { OperationExplanation } from "./api";

export function agentIdentityText(operation: Pick<OperationExplanation,"attribution_status"|"agent_version_id">): string {
  if (operation.attribution_status === "attributed" && operation.agent_version_id) {
    return `Attributed to registered agent version ${operation.agent_version_id}`;
  }
  return `No specific agent independently attributed${operation.agent_version_id?`; registered or claimed version ${operation.agent_version_id}`:""}`;
}

export function actionSummary(operation: Pick<OperationExplanation,"gateway_decision"|"execution_status"|"observation_status">): string {
  if (operation.gateway_decision === "denied") return "Denied by the connected gateway; no execution is established by that decision.";
  if (operation.observation_status === "authenticated Kubernetes audit event observed") return "An authenticated Kubernetes audit event was observed. Resulting state and service health require separate evidence.";
  if (operation.execution_status.includes("receipt")) return "A signed execution record exists; independent Kubernetes observation remains unverified.";
  return "No independently observed mutation is established for this request.";
}

export function attemptText(operation: Pick<OperationExplanation,"key"|"attempted">): string {
  if (operation.key.startsWith("gateway_preflight:") && operation.attempted) return "Requested through connected gateway";
  if (operation.key.startsWith("canonical_request:")) return "Canonical request recorded; gateway linkage unverified";
  return "No linked gateway request";
}
