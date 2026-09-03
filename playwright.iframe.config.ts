import { defineConfig } from "@playwright/test";

/**
 * Local-only config for the self-contained iframe-scroll-bridge spec: it uses
 * page.setContent and needs no app server, and it runs against the system
 * Google Chrome (channel: "chrome") so the Playwright-bundled headless shell
 * does not need to be downloaded in offline/sandboxed environments. Not used
 * by the main `make check` pipeline.
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: /iframe-scroll-bridge\.spec\.ts/,
  timeout: 60000,
  workers: 1,
  retries: 0,
  use: {
    channel: "chrome",
    headless: true,
  },
  projects: [{ name: "chrome", use: { browserName: "chromium", channel: "chrome" } }],
});
