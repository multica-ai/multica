import { type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

function getE2EIdentity(scope: string | number = "default") {
  const suffix = String(scope).replace(/[^a-zA-Z0-9-]+/g, "-");
  return {
    name: `E2E User ${suffix}`,
    email: `e2e+${suffix}@multica.ai`,
    workspaceName: `E2E Workspace ${suffix}`,
    workspaceSlug: `e2e-workspace-${suffix}`,
  };
}

/**
 * Log in as the default E2E user and ensure the workspace exists first.
 * Authenticates via API (send-code → DB read → verify-code), then injects
 * the token into localStorage so the browser session is authenticated.
 */
export async function loginAsDefault(page: Page, scope?: string | number) {
  const identity = getE2EIdentity(scope);
  const api = new TestApiClient();
  await api.login(identity.email, identity.name);
  const workspace = await api.ensureWorkspace(
    identity.workspaceName,
    identity.workspaceSlug,
  );

  const token = api.getToken();
  if (!token) {
    throw new Error("Missing E2E auth token");
  }
  await page.goto("/login");
  await page.evaluate(({ token, workspaceId }) => {
    localStorage.removeItem("multica_issues_view");
    localStorage.removeItem("multica_issue_list_view");
    localStorage.removeItem("multica_issues_scope");
    localStorage.removeItem("multica_my_issues_view");
    localStorage.removeItem("multica_backlog_view");
    localStorage.removeItem("multica_today_view");
    localStorage.removeItem("multica_upcoming_view");
    localStorage.setItem("multica_token", token);
    localStorage.setItem("multica_workspace_id", workspaceId);
  }, { token, workspaceId: workspace.id });
  await page.goto("/issues");
  await page.waitForURL("**/issues", { timeout: 10000 });
  if (/\/login(?:\?|$)/.test(page.url())) {
    await page.goto("/issues");
    await page.waitForURL("**/issues", { timeout: 10000 });
  }
  const desktopMenu = page.getByRole("button", { name: "Workspace menu" });
  const mobileMenu = page.getByRole("button", { name: "Open navigation" });
  const issuesHeading = page.getByRole("heading", { name: "Issues" }).first();

  const desktopReady = desktopMenu
    .waitFor({ state: "visible", timeout: 10000 })
    .then(() => true)
    .catch(() => false);
  const mobileReady = mobileMenu
    .waitFor({ state: "visible", timeout: 10000 })
    .then(() => true)
    .catch(() => false);
  const pageReady = issuesHeading
    .waitFor({ state: "visible", timeout: 10000 })
    .then(() => true)
    .catch(() => false);

  const [hasDesktopMenu, hasMobileMenu, hasPageHeading] = await Promise.all([
    desktopReady,
    mobileReady,
    pageReady,
  ]);

  if (!hasDesktopMenu && !hasMobileMenu && !hasPageHeading) {
    if (/\/login(?:\?|$)/.test(page.url())) {
      await page.goto("/issues");
      await page.waitForURL("**/issues", { timeout: 10000 });

      const retriedDesktop = await desktopMenu
        .waitFor({ state: "visible", timeout: 10000 })
        .then(() => true)
        .catch(() => false);
      const retriedMobile = await mobileMenu
        .waitFor({ state: "visible", timeout: 10000 })
        .then(() => true)
        .catch(() => false);
      const retriedHeading = await issuesHeading
        .waitFor({ state: "visible", timeout: 10000 })
        .then(() => true)
        .catch(() => false);

      if (retriedDesktop || retriedMobile || retriedHeading) {
        return;
      }
    }
    throw new Error("Workspace navigation did not appear");
  }
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(scope?: string | number): Promise<TestApiClient> {
  const identity = getE2EIdentity(scope);
  const api = new TestApiClient();
  await api.login(identity.email, identity.name);
  await api.ensureWorkspace(identity.workspaceName, identity.workspaceSlug);
  return api;
}

export async function openWorkspaceMenu(page: Page) {
  await page.getByRole("button", { name: "Workspace menu" }).click();
  await page.getByRole("menuitem", { name: "Log out" }).waitFor({ state: "visible" });
}
