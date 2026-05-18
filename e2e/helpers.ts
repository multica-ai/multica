import { expect, type Page } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E User";
const E2E_WORKER = process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? "0";
const E2E_RUN_ID = process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const DEFAULT_E2E_EMAIL = `e2e-${E2E_WORKER}-${E2E_RUN_ID}@multica.ai`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-${E2E_WORKER}-${E2E_RUN_ID}`;
// Cookie-auth e2e fixtures (added by this PR). Kept parallel-safe in the same
// worker/run-id scheme as the default fixtures so concurrent CI workers don't
// collide on the same email/workspace.
const COOKIE_E2E_EMAIL = `e2e-cookie-${E2E_WORKER}-${E2E_RUN_ID}@multica.ai`;
const COOKIE_E2E_WORKSPACE = `e2e-cookie-workspace-${E2E_WORKER}-${E2E_RUN_ID}`;
const API_BASE =
  process.env.NEXT_PUBLIC_API_URL ||
  `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function waitForIssuesPage(page: Page) {
  await waitForPageText(page, "New Issue");
  await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible({
    timeout: 15000,
  });
}

export async function waitForPageText(page: Page, text: string, timeout = 30000) {
  await page.waitForFunction(
    (expected) => document.body?.innerText.includes(expected),
    text,
    { timeout },
  );
}

export async function reloadAppPage(page: Page) {
  await page.reload({ waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Issues");
}

/**
 * Log in as the default E2E user and ensure the workspace exists first.
 * Authenticates via API (send-code → DB read → verify-code), then injects
 * the token into localStorage so the browser session is authenticated.
 *
 * Returns the E2E workspace slug so callers can build workspace-scoped URLs.
 */
export async function loginAsDefault(page: Page): Promise<string> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  const workspace = await api.ensureWorkspace(
    `E2E Workspace ${E2E_WORKER}`,
    DEFAULT_E2E_WORKSPACE,
  );
  await api.markUserOnboarded();

  const token = api.getToken();
  if (!token) {
    throw new Error("E2E login did not return an auth token");
  }

  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
  await waitForIssuesPage(page);
  return workspace.slug;
}

/**
 * Log in through the browser API context so HttpOnly auth cookies are written
 * to the Playwright browser context. This intentionally leaves
 * localStorage.multica_token empty to exercise the modern cookie-auth path.
 */
export async function loginWithCookieSession(page: Page): Promise<string> {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    await client.query("DELETE FROM verification_code WHERE email = $1", [
      COOKIE_E2E_EMAIL,
    ]);

    const sendRes = await page.context().request.post(`${API_BASE}/auth/send-code`, {
      data: { email: COOKIE_E2E_EMAIL },
    });
    if (!sendRes.ok()) {
      throw new Error(`send-code failed: ${sendRes.status()}`);
    }

    const result = await client.query(
      "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
      [COOKIE_E2E_EMAIL],
    );
    if (result.rows.length === 0) {
      throw new Error(`No verification code found for ${COOKIE_E2E_EMAIL}`);
    }

    const verifyRes = await page
      .context()
      .request.post(`${API_BASE}/auth/verify-code`, {
        data: { email: COOKIE_E2E_EMAIL, code: result.rows[0].code },
      });
    if (!verifyRes.ok()) {
      throw new Error(`verify-code failed: ${verifyRes.status()}`);
    }
    const login = (await verifyRes.json()) as {
      token: string;
      user: { id: string };
    };
    await client.query("DELETE FROM verification_code WHERE email = $1", [
      COOKIE_E2E_EMAIL,
    ]);
    await client.query(
      'UPDATE "user" SET onboarded_at = COALESCE(onboarded_at, now()) WHERE id = $1',
      [login.user.id],
    );

    const headers = { Authorization: `Bearer ${login.token}` };
    const listRes = await page
      .context()
      .request.get(`${API_BASE}/api/workspaces`, { headers });
    if (!listRes.ok()) {
      throw new Error(`workspace list failed: ${listRes.status()}`);
    }
    const workspaces = (await listRes.json()) as Array<{
      id: string;
      name: string;
      slug: string;
    }>;
    const existing = workspaces.find((item) => item.slug === COOKIE_E2E_WORKSPACE);
    if (existing) return existing.slug;

    const createRes = await page.context().request.post(`${API_BASE}/api/workspaces`, {
      headers,
      data: { name: "E2E Cookie Workspace", slug: COOKIE_E2E_WORKSPACE },
    });
    if (!createRes.ok()) {
      throw new Error(`workspace create failed: ${createRes.status()}`);
    }
    const created = (await createRes.json()) as { slug: string };
    return created.slug;
  } finally {
    await client.end();
  }
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace(`E2E Workspace ${E2E_WORKER}`, DEFAULT_E2E_WORKSPACE);
  await api.markUserOnboarded();
  return api;
}

export async function preferManualCreateMode(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem(
      "multica_create_mode",
      JSON.stringify({ state: { lastMode: "manual" }, version: 0 }),
    );
  });
  await reloadAppPage(page);
  await waitForIssuesPage(page);
}

export async function openWorkspaceMenu(page: Page) {
  // Click the workspace switcher button (has ChevronDown icon)
  const workspaceButton = page.getByRole("button", { name: /E2E Workspace/ }).first();
  await expect(workspaceButton).toBeVisible({ timeout: 15000 });
  await workspaceButton.click();
  // Wait for dropdown to appear
  await expect(page.locator('[class*="popover"]')).toBeVisible();
}
