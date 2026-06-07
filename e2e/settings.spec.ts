import { test, expect } from "@playwright/test";
import { gotoAppPage, loginAsDefault } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    test.setTimeout(60000);

    await loginAsDefault(page);

    // Read the current workspace name from the sidebar
    await expect(
      page.getByRole("button", { name: /E2E Workspace/ }),
    ).toBeVisible();
    const originalName = "E2E Workspace";

    // Navigate to settings
    const settingsHref = await page
      .getByRole("link", { name: "Settings" })
      .getAttribute("href");
    if (!settingsHref) throw new Error("Settings sidebar link has no href");
    await gotoAppPage(page, `${settingsHref}?tab=workspace`);
    await expect(page).toHaveURL(/\/settings\?tab=workspace/, { timeout: 15000 });
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole("tab", { name: "General" })).toHaveAttribute(
      "aria-selected",
      "true",
      { timeout: 15000 },
    );

    // Change workspace name
    const generalPanel = page.getByRole("tabpanel", { name: "General" });
    const nameInput = generalPanel.locator('input[type="text"]').first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);
    await expect(nameInput).toHaveValue(newName);

    // Save
    const saveButton = generalPanel.getByRole("button", {
      name: "Save",
      exact: true,
    });
    await expect(saveButton).toBeEnabled();
    await saveButton.click();

    await expect(page.getByText("Workspace settings saved").first()).toBeVisible({
      timeout: 5000,
    });

    // Sidebar should reflect the new name WITHOUT page refresh
    await expect(
      page.getByRole("button", { name: new RegExp(newName) }),
    ).toBeVisible();

    // Restore original name so other tests aren't affected
    await nameInput.clear();
    await nameInput.fill(originalName);
    await expect(nameInput).toHaveValue(originalName);
    await expect(saveButton).toBeEnabled();
    await saveButton.click();
    await expect(page.getByText("Workspace settings saved").first()).toBeVisible({
      timeout: 5000,
    });
  });
});
