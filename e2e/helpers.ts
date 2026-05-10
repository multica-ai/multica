import { expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E User";
const DEFAULT_E2E_EMAIL = "e2e@multica.ai";
const DEFAULT_E2E_WORKSPACE = "e2e-workspace";

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
    "E2E Workspace",
    DEFAULT_E2E_WORKSPACE,
  );
  await api.dismissStarterContent();

  const token = api.getToken();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
  await page.waitForURL("**/issues", { timeout: 15000 });
  await expect(page.getByRole("link", { name: "Inbox" })).toBeVisible({ timeout: 15000 });
  return workspace.slug;
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace("E2E Workspace", DEFAULT_E2E_WORKSPACE);
  await api.dismissStarterContent();
  return api;
}

export async function openWorkspaceMenu(page: Page) {
  // Click the workspace switcher button (has ChevronDown icon)
  await page.locator('[data-sidebar="menu-button"]').first().click();
  // Wait for dropdown to appear
  await page.getByRole("menuitem", { name: "Log out" }).waitFor({ state: "visible" });
}
