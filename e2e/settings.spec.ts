import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Settings", () => {
  test("updating workspace name reflects in sidebar immediately", async ({
    page,
  }) => {
    await loginAsDefault(page);

    const workspaceButton = page.getByRole("button").first();
    const originalName = (await workspaceButton.innerText()).trim();

    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");
    await page.getByRole("tab", { name: "General" }).click();

    const nameInput = page.getByRole("textbox").first();
    await nameInput.clear();
    const newName = "Renamed WS " + Date.now();
    await nameInput.fill(newName);

    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Workspace settings saved")).toBeVisible({
      timeout: 5000,
    });

    await expect(
      page.getByRole("button", { name: new RegExp(newName, "i") }),
    ).toBeVisible();

    await nameInput.clear();
    await nameInput.fill(originalName);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Workspace settings saved")).toBeVisible({
      timeout: 5000,
    });
  });
});
