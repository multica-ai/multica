/**
 * Seed test fixture for Workflow Panorama View E2E tests.
 *
 * Provides pre-authenticated fixtures using the demo workspace account:
 * - `seededApi` — TestApiClient with lifecycle management (auto-cleanup)
 * - `slug`      — workspace slug "demo111" for URL building
 *
 * Uses account: kdemo648@gmail.com / demo111 workspace
 *
 * Export: `test` (extended Playwright test) and `expect`.
 */

import { test as baseTest, expect } from "@playwright/test";
import { TestApiClient } from "../fixtures";
import type { Page } from "@playwright/test";

// ── Demo account constants ──
const DEMO_EMAIL = "kdemo648@gmail.com";
const DEMO_NAME = "KDemo";
const DEMO_WORKSPACE = "demo111";

/**
 * Log in as the demo user (kdemo648) and ensure the demo111 workspace exists.
 */
export async function loginAsDemo(page: Page): Promise<string> {
  const api = new TestApiClient();
  await api.login(DEMO_EMAIL, DEMO_NAME);
  const workspace = await api.ensureWorkspace("Demo Workspace", DEMO_WORKSPACE);

  const token = api.getToken();
  await page.goto("/login");
  await page.evaluate((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto(`/${workspace.slug}/issues`);
  await page.waitForURL("**/issues", { timeout: 10000 });
  return workspace.slug;
}

/**
 * Create a TestApiClient logged in as the demo user.
 */
export async function createDemoApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEMO_EMAIL, DEMO_NAME);
  await api.ensureWorkspace("Demo Workspace", DEMO_WORKSPACE);
  return api;
}

// ── Extended test fixture ──

interface PanoramaFixtures {
  seededApi: TestApiClient;
  slug: string;
}

const test = baseTest.extend<PanoramaFixtures>({
  seededApi: async ({}, use) => {
    const api = await createDemoApi();
    await use(api);
    await api.cleanup();
  },

  slug: async ({ page }, use) => {
    const s = await loginAsDemo(page);
    await use(s);
  },
});

export { test, expect, DEMO_EMAIL, DEMO_NAME, DEMO_WORKSPACE };
export { seedFullPanoramaWorkflow, FULL_PANORAMA_STATS } from "./seed-full-panorama";
export type { FullPanoramaSeed, PanoramaSeedAgent, PanoramaSeedStage, PanoramaSeedNode, PanoramaSeedEdge } from "./seed-full-panorama";
