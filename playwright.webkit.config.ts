import "./e2e/env";
import { defineConfig } from "@playwright/test";

/**
 * MUL-7095 / PR #8092: WebKit first-edit and drop determinism is a merge
 * blocker for `description-reentry.spec.ts` (10 consecutive clean passes),
 * but it is not a second axis for the whole suite. The default config stays
 * Chromium-only so a bare `playwright test` neither doubles the canonical
 * suite nor starts requiring a browser CI does not install; WebKit runs from
 * this config, scoped to that spec.
 *
 * `description-navigation-trace.spec.ts` is deliberately NOT matched here:
 * it is a Long Tasks API recorder (`PerformanceObserver` `longtask`), an
 * entry type WebKit does not implement, so its acceptance is Chromium-only.
 *
 *   pnpm exec playwright test --config=playwright.webkit.config.ts
 *
 * Needs the WebKit browser (`pnpm exec playwright install --with-deps webkit`)
 * and the same already-running servers as the default config. `make check`
 * runs it as its own E2E step.
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: /description-reentry\.spec\.ts/,
  timeout: 60000,
  workers: 1,
  retries: 0,
  use: {
    baseURL:
      process.env.PLAYWRIGHT_BASE_URL ??
      process.env.FRONTEND_ORIGIN ??
      "http://localhost:3000",
    headless: true,
  },
  projects: [{ name: "webkit", use: { browserName: "webkit" } }],
});
