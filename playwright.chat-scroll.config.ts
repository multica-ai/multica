import { defineConfig } from "@playwright/test";

// Self-contained real-Virtuoso tests; no app server, API, or credentials.
export default defineConfig({
  testDir: "./e2e",
  testMatch: "chat-scroll.spec.ts",
  workers: 1,
  retries: 0,
  timeout: 30000,
  use: { headless: true, browserName: "chromium" },
});
