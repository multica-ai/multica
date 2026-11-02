import { defineConfig } from "@playwright/test";

// scripts/test-retained-e2e.sh owns the build, migration and fixed test endpoints.
// Do not inherit playwright.config.ts's dotenv loading or development servers.
export default defineConfig({
  testDir: "./e2e",
  testMatch: ["onboarding-smoke.spec.ts", "dag-task-lines.spec.ts", "issue-dependencies.spec.ts", "comments.spec.ts"],
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  outputDir: "test-results/retained-e2e/results",
  reporter: [
    ["list"],
    ["html", { outputFolder: "test-results/retained-e2e/report", open: "never" }],
  ],
  use: {
    baseURL: "http://127.0.0.1:13000",
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: [
    {
      command: "./bin/retained-e2e-server",
      cwd: "server",
      url: "http://127.0.0.1:18080/health",
      reuseExistingServer: false,
      timeout: 60_000,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
    },
    {
      command: "pnpm --filter @multica/web exec next start --hostname 127.0.0.1 --port 13000",
      url: "http://127.0.0.1:13000/login",
      reuseExistingServer: false,
      timeout: 60_000,
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
    },
  ],
});
