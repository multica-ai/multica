import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page);

    // Read the current workspace name from the sidebar
    const sidebarName = page.locator('[data-sidebar="menu-button"]').first();
    const originalName = await sidebarName.innerText();

    // Navigate to settings
    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");
    await page.getByRole("tab", { name: "General" }).click();

    // Change workspace name
    const nameInput = page.locator('input[type="text"]').first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    // Save
    await page.getByRole("button", { name: "Save", exact: true }).click();

    // Wait for "Saved!" confirmation
    await expect(page.getByText("Workspace settings saved").last()).toBeVisible({ timeout: 5000 });

    // Sidebar should reflect the new name WITHOUT page refresh
    await expect(page.getByRole("button", { name: new RegExp(newName) })).toBeVisible();

    // Restore original name so other tests aren't affected
    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Workspace settings saved").last()).toBeVisible({ timeout: 5000 });
  });
});
