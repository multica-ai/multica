import { type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E User";
const E2E_RUN_ID = `${process.pid}-${Date.now()}`;
const DEFAULT_E2E_EMAIL = `e2e-${E2E_RUN_ID}@multica.ai`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-${E2E_RUN_ID}`;

/**
 * Log in as the default E2E user and ensure the workspace exists first.
 * Authenticates via API (send-code → DB read → verify-code), then injects
 * the token into localStorage so the browser session is authenticated.
 */
export async function loginAsDefault(page: Page) {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  const workspace = await api.ensureWorkspace(
    "E2E Workspace",
    DEFAULT_E2E_WORKSPACE,
  );

  const token = api.getToken();
  if (!token) throw new Error("login did not return a token");

  await page.addInitScript(
    ({ token, workspaceId }) => {
      localStorage.setItem("multica_token", token);
      localStorage.setItem("multica_workspace_id", workspaceId);
    },
    { token, workspaceId: workspace.id },
  );
  await page.goto("/issues");
  await page.waitForURL("**/issues", { timeout: 10000 });
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace("E2E Workspace", DEFAULT_E2E_WORKSPACE);
  return api;
}

export function workspaceMenuButton(page: Page) {
  return page.locator('[data-sidebar="menu-button"]').first();
}

export async function openWorkspaceMenu(page: Page) {
  await workspaceMenuButton(page).click();
  await page.getByRole("menuitem", { name: "Log out" }).waitFor();
}
