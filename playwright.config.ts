import "./e2e/env";
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  retries: 0,
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? process.env.FRONTEND_ORIGIN ?? "http://localhost:3000",
    headless: true,
  },
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
    // The iPhone runs the Safari/WebKit engine, where overflow + flexbox
    // min-width behave differently than Chromium. Run the mobile message
    // overflow guard on WebKit too so a Chrome-only "looks fine" can't let a
    // phone-width clipping regression slip through. Scoped to that spec only
    // to keep the rest of the suite Chromium-only.
    {
      name: "webkit-mobile",
      testMatch: /message-overflow-mobile\.spec\.ts$/,
      use: { browserName: "webkit" },
    },
  ],
  // Don't auto-start servers — they must be running already
  // This avoids complexity and port conflicts during testing
});
