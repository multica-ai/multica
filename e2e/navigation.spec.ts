import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDefault(page);
  });

  test("sidebar navigation works", async ({ page }) => {
    // Click Inbox
    await page.locator("nav a", { hasText: "Inbox" }).click();
    await page.waitForURL("**/inbox");
    await expect(page).toHaveURL(/\/inbox/);

    // Click Agents
    await page.locator("nav a", { hasText: "Agents" }).click();
    await page.waitForURL("**/agents");
    await expect(page).toHaveURL(/\/agents/);

    // Click Issues
    await page.locator("nav a", { hasText: "Issues" }).click();
    await page.waitForURL("**/issues");
    await expect(page).toHaveURL(/\/issues/);
  });

  test("settings page loads via workspace menu", async ({ page }) => {
    // Settings is inside the workspace dropdown menu
    await openWorkspaceMenu(page);
    await page.locator("text=Settings").click();
    await page.waitForURL("**/settings");

    await expect(page.getByRole("heading", { name: "Workspace" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Members" })).toBeVisible();
  });

  test("agents page shows agent list", async ({ page }) => {
    await page.locator("nav a", { hasText: "Agents" }).click();
    await page.waitForURL("**/agents");

    // Should show "Agents" heading
    await expect(page.locator("text=Agents").first()).toBeVisible();
  });
});
