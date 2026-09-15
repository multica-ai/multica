import "./e2e/env";
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  // The performance scenario has its own config: one worker, no retries, and a
  // running production build. It must not be swept up by the ordinary suite.
  testIgnore: "**/perf/**",
  timeout: 60000,
  workers: 1,
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
    // MUL-7095 / PR #8092 Revision 3 §4: the same description specs also
    // run on WebKit, where first edit/drop determinism is a merge blocker
    // (spec: 10 consecutive passes from a clean checkout). WebKit browsers
    // install via `pnpm exec playwright install --with-deps webkit`; the
    // Chromium project stays first so a bare `playwright test` keeps its
    // existing default-project behavior.
    {
      name: "webkit",
      use: { browserName: "webkit" },
    },
  ],
  // Don't auto-start servers — they must be running already
  // This avoids complexity and port conflicts during testing
});
