const { defineConfig } = require("@playwright/test");
const path = require("node:path");

module.exports = defineConfig({
  testDir: ".",
  testMatch: "operation-explanations.spec.cjs",
  timeout: 45_000,
  expect: { timeout: 10_000 },
  use: { baseURL: "http://127.0.0.1:3000", trace: "retain-on-failure", screenshot: "only-on-failure" },
  reporter: [["list"], [path.resolve(__dirname, "operation-phase-reporter.cjs")], ["junit", { outputFile: "test-results/operation-explanations.xml" }]],
});
