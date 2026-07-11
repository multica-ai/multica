import { test, expect } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

const ROUTE_CHANGE_TIMEOUT = 30000;

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDefault(page);
    await page.waitForLoadState("networkidle");
  });

  test("sidebar navigation works", async ({ page }) => {
    await page.getByRole("link", { name: "Inbox" }).click();
    await expect(page).toHaveURL(/\/inbox/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "Inbox");

    await page.getByRole("link", { name: "Agents" }).click();
    await expect(page).toHaveURL(/\/agents/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "Agents");

    await page.getByRole("link", { name: "Issues", exact: true }).click();
    await expect(page).toHaveURL(/\/issues/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "Issues");
  });

  test("settings page loads via sidebar", async ({ page }) => {
    await page.getByLabel("Settings").click();
    await expect(page).toHaveURL(/\/settings/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "Settings");

    const settingsNav = page.getByRole("navigation", { name: "Settings" });
    await expect(settingsNav.getByRole("heading", { name: "My Account", exact: true })).toBeVisible();
    await expect(settingsNav.getByRole("heading", { name: "Workspace", exact: true })).toBeVisible();
    await expect(settingsNav.getByRole("heading", { name: "Space", exact: true })).toBeVisible();
    await expect(settingsNav.getByRole("link", { name: "General" })).toBeVisible();
    await expect(settingsNav.getByRole("link", { name: "Members" })).toBeVisible();

    const spaceSettingsLink = settingsNav.locator('a[href*="/settings/space/"]').first();
    await expect(spaceSettingsLink).toBeVisible();
    const canonicalHref = await spaceSettingsLink.getAttribute("href");
    if (!canonicalHref) throw new Error("Space Settings link has no href");
    const canonicalUrl = new URL(canonicalHref, page.url()).toString();

    await spaceSettingsLink.click();
    await expect(page).toHaveURL(/\/settings\/space\/[A-Z0-9]+$/, {
      timeout: ROUTE_CHANGE_TIMEOUT,
    });
    await waitForPageText(page, "Agent Context");

    // Old bookmarks remain valid but land in the unified Settings surface.
    const route = canonicalHref.match(/^(.*)\/settings\/space\/([^/]+)$/);
    if (!route) throw new Error(`Unexpected Space Settings href: ${canonicalHref}`);
    await page.goto(`${route[1]}/space/${route[2]}/settings`);
    await expect(page).toHaveURL(canonicalUrl, { timeout: ROUTE_CHANGE_TIMEOUT });
  });

  test("agents page shows agent list", async ({ page }) => {
    await page.getByRole("link", { name: "Agents" }).click();
    await expect(page).toHaveURL(/\/agents/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "Agents");

    // Should show "Agents" heading
    await expect(page.locator("text=Agents").first()).toBeVisible();
  });
});
