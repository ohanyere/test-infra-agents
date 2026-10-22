import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "operation-explanations.spec.mjs",
  timeout: 45_000,
  expect: { timeout: 10_000 },
  use: { baseURL: "http://127.0.0.1:3000", trace: "retain-on-failure", screenshot: "only-on-failure" },
  reporter: [["list"], ["junit", { outputFile: "test-results/operation-explanations.xml" }]],
});
