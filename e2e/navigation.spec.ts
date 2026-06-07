import { test, expect } from "@playwright/test";
import { loginAsDefault, openSidebarLink } from "./helpers";

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDefault(page);
  });

  test("sidebar navigation works", async ({ page }) => {
    test.setTimeout(60000);

    await openSidebarLink(page, "Inbox", /\/inbox/);
    await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible({
      timeout: 15000,
    });

    await openSidebarLink(page, "Agents", /\/agents/);
    await expect(
      page.getByRole("heading", { name: "Agents", exact: true }),
    ).toBeVisible({ timeout: 15000 });

    await openSidebarLink(page, "Issues", /\/issues/, { exact: true });
    await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible({
      timeout: 15000,
    });
  });

  test("settings page loads from sidebar", async ({ page }) => {
    await openSidebarLink(page, "Settings", /\/settings/);

    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "General" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Members" })).toBeVisible();
  });

  test("agents page shows agent list", async ({ page }) => {
    await openSidebarLink(page, "Agents", /\/agents/);

    // Should show "Agents" heading
    await expect(
      page.getByRole("heading", { name: "Agents", exact: true }),
    ).toBeVisible();
  });
});
