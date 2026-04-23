import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDefault(page);
  });

  test("sidebar navigation works", async ({ page }) => {
    await page.getByRole("link", { name: /Inbox/ }).click();
    await page.waitForURL("**/inbox");
    await expect(page).toHaveURL(/\/inbox/);

    await page.getByRole("link", { name: /Agents/ }).click();
    await page.waitForURL("**/agents");
    await expect(page).toHaveURL(/\/agents/);

    await page.getByRole("link", { name: /^Issues$/ }).click();
    await page.waitForURL("**/issues");
    await expect(page).toHaveURL(/\/issues/);
  });

  test("settings page loads via workspace menu", async ({ page }) => {
    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");

    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "General" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Members" })).toBeVisible();
  });

  test("agents page shows agent list", async ({ page }) => {
    await page.getByRole("link", { name: /Agents/ }).click();
    await page.waitForURL("**/agents");

    await expect(page.getByText("Agents").first()).toBeVisible();
  });
});
