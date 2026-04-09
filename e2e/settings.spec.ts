import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page, test.info().parallelIndex);

    const workspaceMenu = page.getByRole("button", { name: "Workspace menu" });
    const originalName = (await workspaceMenu.innerText()).trim();

    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");

    await page.getByRole("tab", { name: "General" }).click();

    const nameInput = page.locator('input[type="text"]').first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    await page.locator("button", { hasText: "Save" }).click();

    await expect(page.getByText("Workspace settings saved").last()).toBeVisible({
      timeout: 5000,
    });

    await expect(workspaceMenu).toContainText(newName);

    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await page.locator("button", { hasText: "Save" }).click();
    await expect(page.getByText("Workspace settings saved").last()).toBeVisible({
      timeout: 5000,
    });
  });

  test("mobile settings uses top tabs above content", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await loginAsDefault(page, test.info().parallelIndex);

    await page.goto("/settings");
    await page.waitForURL(/\/settings$/);

    const tabsList = page.locator('[data-slot="tabs-list"]').first();
    const contentHeading = page.getByRole("heading", { name: "Profile" });

    const tabsBox = await tabsList.boundingBox();
    const headingBox = await contentHeading.boundingBox();

    expect(tabsBox).not.toBeNull();
    expect(headingBox).not.toBeNull();
    expect(headingBox!.y).toBeGreaterThan((tabsBox!.y + tabsBox!.height) - 1);
  });
});
