import { readFileSync, mkdirSync, appendFileSync } from "node:fs";
import { test, expect } from "@playwright/test";

function record(phase) {
  const path = process.env.OPERATION_PHASE_FILE;
  if (path) appendFileSync(path, `${new Date().toISOString()} ${phase}\n`);
}

record("spec module loaded");
const fixture = JSON.parse(readFileSync(process.env.OPERATION_PROOF_FILE, "utf8"));
const password = process.env.OPERATION_PROOF_PASSWORD;
const api = "http://127.0.0.1:8080";
if (!password || fixture.source_classification !== "INSUFFICIENT_PROVENANCE") throw new Error("disposable provisional proof fixture required");

async function login(page, email) {
  record("page fixture ready; login navigation starting");
  await page.goto("/login");
  record("login page loaded");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/workspace/);
  record("authentication completed");
}

const sessionPath = `/assurance/${fixture.environment_id}/sessions/${fixture.session_id}`;
const evidenceURL = `${api}/v1/workspaces/${fixture.workspace_id}/customer-kubernetes-environments/${fixture.environment_id}/observation-sessions/${fixture.session_id}/evidence-detail?reference=${encodeURIComponent(fixture.evidence_reference)}`;

test("owner sees exact provisional operation, rule, and scoped evidence", async ({ page }) => {
  await login(page, fixture.email);
  record("owner assertions starting");
  await page.goto(sessionPath);
  await expect(page.getByRole("heading", { name: "What happened in this session" })).toBeVisible();
  const operation = page.locator("article").filter({ hasText: `audit:${fixture.audit_id}` });
  await expect(operation).toHaveCount(1);
  await expect(operation).toContainText("No authenticated requested action linked");
  await expect(operation).toContainText(`${fixture.namespace}/${fixture.resource_name}`);
  await expect(operation).toContainText("No linked gateway request");
  await expect(operation).toContainText("origin unverified");
  await expect(operation).toContainText("No specific agent independently attributed");
  await expect(operation).toContainText("not established");
  await expect(operation).toContainText(fixture.rule_id);
  await expect(page.getByText("UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE")).toBeVisible();
  await operation.getByText("Supporting records").click();
  await operation.getByRole("button", { name: fixture.evidence_reference }).click();
  await expect(operation).toContainText("API-server origin unverified");
  const evidence = await page.context().request.get(evidenceURL);
  expect(evidence.status()).toBe(200);
  const detail = await evidence.json();
  expect(detail.reference).toBe(fixture.evidence_reference);
  expect(detail.digest).toBe(fixture.source_event_digest);
  const reports = await page.context().request.get(`${api}/v1/workspaces/${fixture.workspace_id}/customer-kubernetes-environments/${fixture.environment_id}/observation-sessions/${fixture.session_id}/operational-assessments`);
  expect(reports.status()).toBe(200);
  const report = (await reports.json()).items.at(-1);
  expect(report.observed_actions).toEqual(expect.arrayContaining([expect.objectContaining({id:fixture.audit_id,operation:"patch",kind:"Deployment",namespace:fixture.namespace,name:fixture.resource_name,origin_authenticated:false})]));
  expect(report.findings).toEqual(expect.arrayContaining([expect.objectContaining({rule_id:fixture.rule_id,code:"UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE",affected_resource:`${fixture.namespace}/${fixture.resource_name}`})]));
  mkdirSync("test-results", { recursive: true });
  await page.screenshot({ path: "test-results/operation-explanation-owner.png", fullPage: true });
});

test("another tenant cannot read the report or evidence", async ({ browser }) => {
  record("second tenant browser fixture ready");
  const context = await browser.newContext();
  record("second tenant context ready");
  const page = await context.newPage();
  try {
    await login(page, fixture.other_email);
    const evidence = await context.request.get(evidenceURL);
    expect(evidence.status()).toBe(403);
    const reports = await context.request.get(`${api}/v1/workspaces/${fixture.workspace_id}/customer-kubernetes-environments/${fixture.environment_id}/observation-sessions/${fixture.session_id}/operational-assessments`);
    expect(reports.status()).toBe(403);
    await page.goto(sessionPath);
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(page.getByRole("heading", { name: "What happened in this session" })).toHaveCount(0);
  } finally {
    await context.close();
  }
});
