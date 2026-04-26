import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu, workspaceMenuButton } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page);

    // Read the current workspace name from the sidebar
    const sidebarName = workspaceMenuButton(page);
    const originalName = await sidebarName.innerText();

    // Navigate to settings
    await openWorkspaceMenu(page);
    await page.locator("text=Settings").click();
    await page.waitForURL("**/settings");
    await page.getByRole("tab", { name: "General" }).click();

    // Change workspace name
    const nameInput = page
      .locator('input[type="text"]')
      .first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    // Save
    await page.locator("button", { hasText: "Save" }).click();

    // Wait for save confirmation
    await expect(
      page.getByText("Workspace settings saved").first(),
    ).toBeVisible({ timeout: 5000 });

    // Sidebar should reflect the new name WITHOUT page refresh
    await expect(sidebarName).toContainText(newName);

    // Restore original name so other tests aren't affected
    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await page.locator("button", { hasText: "Save" }).click();
    await expect(
      page.getByText("Workspace settings saved").first(),
    ).toBeVisible({ timeout: 5000 });
  });
});
