import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const requiredRoutes = [
  "app/workspace/page.tsx", "app/workspace/settings/page.tsx", "app/workspace/billing/page.tsx",
  "app/workspace/tasks/page.tsx", "app/workspace/tasks/[id]/page.tsx",
  "app/agents/page.tsx", "app/agents/new/page.tsx", "app/agents/[id]/page.tsx",
  "app/qualification/page.tsx", "app/qualification/suites/[id]/page.tsx",
  "app/runs/page.tsx", "app/runs/[id]/page.tsx", "app/runs/[id]/trajectory/page.tsx",
  "app/verifications/page.tsx", "app/verifications/[id]/page.tsx", "app/integrations/page.tsx",
  "app/suite-executions/[id]/page.tsx", "app/comparisons/[id]/page.tsx",
];
const deletedMockModules = [
  "app/agents/agents-data.ts", "app/scenarios/scenarios-data.ts", "app/runs/[id]/run-data.ts",
  "app/evaluations/[id]/evaluation-data.ts", "app/comparisons/[id]/comparison-data.ts",
];
const failures = [];
for (const path of requiredRoutes) if (!existsSync(path)) failures.push(`missing critical route: ${path}`);
for (const path of deletedMockModules) if (existsSync(path)) failures.push(`obsolete customer mock restored: ${path}`);

const productionSources = requiredRoutes.filter(existsSync).map((path) => [path, readFileSync(path, "utf8")]);
for (const [path, source] of productionSources) {
  if (/agents-data|scenarios-data|run-data|evaluation-data|comparison-data/.test(source)) failures.push(`customer route imports obsolete mock data: ${path}`);
}
const agentDetail = readFileSync(join("app", "agents", "[id]", "agent-detail.tsx"), "utf8");
if (!agentDetail.includes('detail?.test_status || "untested"')) failures.push("TESTED must fail closed to authoritative capability coverage");
if (/detail\?\.test_status\s*\|\|\s*claim\.test_status/.test(agentDetail)) failures.push("TESTED must not fall back to claim presence/projection");

if (failures.length) {
  for (const failure of failures) console.error(`integrity: ${failure}`);
  process.exit(1);
}
console.log(`integrity: ${requiredRoutes.length} critical routes present; customer mock imports absent; TESTED coverage source guarded`);
