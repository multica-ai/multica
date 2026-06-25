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
 * Log in as the demo user (kdemo648) by setting auth cookies directly.
 *
 * Uses TestApiClient for API auth (token), then injects the token as
 * HttpOnly cookies into the browser context. This avoids the UI login
 * flow entirely and eliminates rate-limit / login-repeat issues.
 */
export async function loginAsDemo(page: Page, api?: TestApiClient): Promise<string> {
  const client = api ?? new TestApiClient();
  await client.login(DEMO_EMAIL, DEMO_NAME);
  await client.ensureWorkspace("Demo Workspace", DEMO_WORKSPACE);

  const token = client.getToken();
  if (!token) throw new Error("Failed to obtain auth token");

  // Set the HttpOnly auth cookie (same name the server uses: multica_auth)
  const frontendOrigin = process.env.FRONTEND_ORIGIN || "http://localhost:3000";
  await page.context().addCookies([
    {
      name: "multica_auth",
      value: token,
      domain: "localhost",
      path: "/",
      httpOnly: true,
      sameSite: "Lax" as const,
      expires: Math.floor(Date.now() / 1000) + 2592000, // 30 days
    },
  ]);

  // Navigate to workspace — should be authenticated now
  await page.goto(`${BASE_PATH}/${DEMO_WORKSPACE}/issues`, { waitUntil: "load", timeout: 15000 });

  return DEMO_WORKSPACE;
}

/**
 * Create a TestApiClient logged in as the demo user.
 * Exported so tests can reuse the same client for API operations.
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

  slug: async ({ page, seededApi }, use) => {
    const s = await loginAsDemo(page, seededApi);
    await use(s);
  },
});

export { test, expect, DEMO_EMAIL, DEMO_NAME, DEMO_WORKSPACE, BASE_PATH };
export { seedFullPanoramaWorkflow, FULL_PANORAMA_STATS } from "./seed-full-panorama";
export type { FullPanoramaSeed, PanoramaSeedAgent, PanoramaSeedStage, PanoramaSeedNode, PanoramaSeedEdge } from "./seed-full-panorama";
