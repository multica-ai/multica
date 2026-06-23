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

/** Next.js base path prefix (e.g. "/tasks") — read from the same env var the app uses. */
const BASE_PATH = process.env.NEXT_PUBLIC_BASE_PATH || "";

/**
 * Log in as the demo user (kdemo648) via UI flow.
 * The web app uses HttpOnly cookies for auth, so we must go through
 * the actual login form rather than setting localStorage tokens.
 */
export async function loginAsDemo(page: Page): Promise<string> {
  // Navigate to login page
  await page.goto(`${BASE_PATH}/login`);
  await page.waitForURL(`**/login`, { timeout: 10000 });

  // Step 1: Enter email
  const emailInput = page.getByPlaceholder("you@example.com");
  await emailInput.fill(DEMO_EMAIL);
  await page.waitForTimeout(300);

  // Step 2: Click Continue
  const continueBtn = page.getByRole("button", { name: /continue|继续/i });
  await continueBtn.click();
  await page.waitForTimeout(500);

  // Step 3: Enter verification code (fixed dev code 123456).
  // The code input is a plain textbox without a placeholder — use role lookup.
  const codeInput = page.getByRole("textbox").last();
  await codeInput.fill("123456");

  // The form auto-submits when a valid 6-digit code is entered.
  // After login, force-navigate to the correct workspace (demo111).
  // The default redirect may land on a different workspace.
  await page.waitForURL(`**/issues`, { timeout: 15000 });
  await page.goto(`${BASE_PATH}/${DEMO_WORKSPACE}/issues`);
  await page.waitForURL(`**/${DEMO_WORKSPACE}/issues`, { timeout: 10000 });

  return DEMO_WORKSPACE;
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

export { test, expect, DEMO_EMAIL, DEMO_NAME, DEMO_WORKSPACE, BASE_PATH };
export { seedFullPanoramaWorkflow, FULL_PANORAMA_STATS } from "./seed-full-panorama";
export type { FullPanoramaSeed, PanoramaSeedAgent, PanoramaSeedStage, PanoramaSeedNode, PanoramaSeedEdge } from "./seed-full-panorama";
