import test from "node:test";
import assert from "node:assert/strict";
import { actionSummary, agentIdentityText, attemptText } from "./operation-explanation.ts";

test("canonical requests are not described as gateway attempts", () => {
  assert.equal(attemptText({key:"canonical_request:action-1",attempted:true}), "Canonical request recorded; gateway linkage unverified");
  assert.equal(attemptText({key:"gateway_preflight:preflight-1",attempted:true}), "Requested through connected gateway");
});

test("denied actions never read as observed changes", () => {
  assert.match(actionSummary({gateway_decision:"denied", execution_status:"denied by connected gateway", observation_status:"not established"}), /no execution is established/);
});

test("receipt alone does not read as Kubernetes observation", () => {
  assert.match(actionSummary({gateway_decision:"allowed", execution_status:"signed execution receipt recorded", observation_status:"not established"}), /remains unverified/);
});

test("audit observation does not read as verified service outcome", () => {
  assert.match(actionSummary({gateway_decision:"allowed", execution_status:"signed execution receipt recorded", observation_status:"authenticated Kubernetes audit event observed"}), /separate evidence/);
});

test("registered or claimed version is not called the actor without attribution", () => {
  assert.match(agentIdentityText({attribution_status:"unresolved or conflicting", agent_version_id:"version-1"}), /No specific agent independently attributed/);
  assert.equal(agentIdentityText({attribution_status:"attributed", agent_version_id:"version-1"}), "Attributed to registered agent version version-1");
});
