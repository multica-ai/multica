import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page);

    const sidebarName = page.getByRole("button", { name: /E2E Workspace/i });
    const originalName = await sidebarName.innerText();

    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    await page.getByRole("tab", { name: "General" }).click();

    const nameInput = page
      .locator('input[type="text"]')
      .first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    await page.locator("button", { hasText: "Save" }).click();

    await expect(
      page.getByText("Workspace settings saved").last(),
    ).toBeVisible({ timeout: 5000 });

    await expect(
      page.getByRole("button", { name: new RegExp(newName) }),
    ).toBeVisible();

    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await page.locator("button", { hasText: "Save" }).click();
    await expect(
      page.getByText("Workspace settings saved").last(),
    ).toBeVisible({ timeout: 5000 });
  });
});
