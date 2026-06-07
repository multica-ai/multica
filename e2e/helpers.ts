import { expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E User";
const WORKER_SUFFIX = (
  process.env.TEST_PARALLEL_INDEX ??
  process.env.TEST_WORKER_INDEX ??
  "0"
).replace(/[^a-zA-Z0-9-]/g, "-");
const DEFAULT_E2E_EMAIL = `e2e-${WORKER_SUFFIX}@multica.ai`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-${WORKER_SUFFIX}`;

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
  await api.completeOnboarding();

  const token = api.getToken();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  await gotoAppPage(page, `/${workspace.slug}/issues`);
  await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible({
    timeout: 15000,
  });
  try {
    await page.waitForLoadState("load", { timeout: 5000 });
  } catch {
    await page.evaluate(() => window.stop());
  }
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
  await api.completeOnboarding();
  return api;
}

export async function openWorkspaceMenu(page: Page) {
  await page.getByRole("button", { name: /E2E Workspace/ }).click();
  await page.getByRole("menuitem", { name: "Log out" }).waitFor({
    state: "visible",
  });
}

export async function openSidebarLink(
  page: Page,
  name: string,
  expectedUrl: RegExp,
  options: { exact?: boolean } = {},
) {
  const link = page.getByRole("link", { name, exact: options.exact });
  await expect(link).toBeVisible();
  const href = await link.getAttribute("href");
  if (!href) throw new Error(`Sidebar link "${name}" has no href`);
  await gotoAppPage(page, href);
  await expect(page).toHaveURL(expectedUrl, { timeout: 15000 });
}

export async function gotoAppPage(page: Page, url: string) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.goto(url, { waitUntil: "domcontentloaded" });
      return;
    } catch (error) {
      const aborted =
        error instanceof Error && error.message.includes("net::ERR_ABORTED");
      if (!aborted || attempt === 2) throw error;
      await page.evaluate(() => window.stop()).catch(() => {});
    }
  }
}
