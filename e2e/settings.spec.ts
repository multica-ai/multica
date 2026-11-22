import { test, expect, type Page } from "@playwright/test";
import { appRail, currentWorkspaceTile, loginAsDefault, waitForPageText } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in the rail immediately", async ({
    page,
  }) => {
    const workspaceSlug = await loginAsDefault(page);

    // Read the current workspace name from its rail tile
    const tile = currentWorkspaceTile(page);
    await expect(tile).toBeVisible({ timeout: 15000 });
    const originalName = (await tile.getAttribute("aria-label")) ?? "E2E Workspace";

    await page.goto(`/${workspaceSlug}/settings?tab=workspace`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "General");

    // Change workspace name
    const nameInput = page
      .locator('input[type="text"]')
      .first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    // Save
    await page.locator("button", { hasText: "Save" }).click();

    await expect(page.getByText("Workspace settings saved").first()).toBeVisible({ timeout: 5000 });

    // The rail should reflect the new name WITHOUT page refresh
    await expect(appRail(page).getByRole("link", { name: newName })).toBeVisible();

    // Restore original name so other tests aren't affected
    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await page.locator("button", { hasText: "Save" }).click();
    await expect(page.getByText("Workspace settings saved").first()).toBeVisible({ timeout: 5000 });
    await expect(appRail(page).getByRole("link", { name: originalName })).toBeVisible();
  });

  // Connected apps are mocked at the network boundary so the flow runs without
  // a configured COMPOSIO_API_KEY or a live Composio project. A connect started
  // without a return path comes back as `/settings?tab=apps&connected=<slug>`
  // (the web proxy prepends the workspace slug); a personal one must land on
  // Connected accounts, report the result once and refresh the list.
  async function mockConnectedApps(page: Page) {
    let connected = false;
    await page.route(
      (url) => url.pathname === "/api/config",
      async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        await route.fulfill({
          response,
          json: { ...body, feature_flags: { ...body.feature_flags, composio_mcp_apps: true } },
        });
      },
    );
    await page.route(
      (url) => url.pathname === "/api/integrations/composio/toolkits",
      (route) => route.fulfill({ json: [{ slug: "notion", name: "Notion", connectable: true }] }),
    );
    await page.route(
      (url) => url.pathname === "/api/apps",
      (route) => route.fulfill({ json: {
        catalog_mode: "all",
        can_manage: true,
        team: [],
        agent_apps: [{ agent_id: "agent-1", toolkit_slug: "notion", account_scope: "personal", level: "write" }],
        personal: connected
          ? [{ id: "conn-notion-1", toolkit_slug: "notion", status: "active", connected_at: new Date().toISOString(), last_used_at: null }]
          : [],
      } }),
    );
    await page.route(
      (url) => url.pathname === "/api/apps/catalog",
      (route) => route.fulfill({ json: {
        catalog_mode: "all",
        apps: [{ slug: "notion", name: "Notion", curated: true, recommended_scope: "team" }],
      } }),
    );
    await page.route(
      (url) => url.pathname === "/api/app-requests",
      (route) => route.fulfill({ json: { requests: [], next_before: null } }),
    );
    await page.route(
      (url) => /^\/api\/integrations\/composio\/connections\/[^/]+\/impact$/.test(url.pathname),
      (route) => route.fulfill({ json: { agents: [] } }),
    );
    await page.route(
      (url) => url.pathname === "/api/apps/personal/connect",
      (route) => {
        connected = true;
        return route.fulfill({ json: { redirect_url: "/settings?tab=apps&connected=notion" } });
      },
    );
  }

  test("the old Accounts settings link opens Apps › Accounts, and a personal connect returns there", async ({
    page,
  }) => {
    await mockConnectedApps(page);
    const workspaceSlug = await loginAsDefault(page);

    await page.goto(`/${workspaceSlug}/settings?tab=accounts`, { waitUntil: "domcontentloaded" });
    await expect(page).toHaveURL(new RegExp(`/${workspaceSlug}/apps\\?tab=accounts$`), { timeout: 10000 });
    await waitForPageText(page, "Your accounts");

    await page.locator('[data-account-row="personal:notion"]').getByRole("button", { name: "Connect" }).click();
    // Agents' abilities are disclosed before the hosted flow starts.
    await page.getByRole("dialog", { name: /Connect Notion/ }).getByRole("button", { name: "Continue" }).click();

    await expect(page.getByText("Account connected").first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole("button", { name: "Notion actions" })).toBeVisible({ timeout: 10000 });
    await expect(page).toHaveURL(/tab=accounts/);
    await expect(page).not.toHaveURL(/connected=notion/);
  });

  test("the retired Apps settings tab opens the Apps page", async ({ page }) => {
    await mockConnectedApps(page);
    const workspaceSlug = await loginAsDefault(page);

    await page.goto(`/${workspaceSlug}/settings?tab=apps`, { waitUntil: "domcontentloaded" });
    await expect(page).toHaveURL(new RegExp(`/${workspaceSlug}/apps$`), { timeout: 10000 });
    await expect(page.getByRole("tab", { name: /All apps/ })).toBeVisible();
  });
});
