import { defineConfig } from "@playwright/test";

if (!process.env.MULTICA_CONVERSATION_FIXTURE_URL) {
  throw new Error("Run through scripts/check-conversation-view-mount.mjs");
}

export default defineConfig({
  testDir: "../e2e",
  testMatch: "agent-gui-direct-mount.spec.ts",
  outputDir: "../.tmp/conversation-view-browser-results",
  workers: 1,
  use: {
    baseURL: process.env.MULTICA_CONVERSATION_FIXTURE_URL,
    channel: "chrome",
    headless: true
  }
});
