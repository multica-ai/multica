import { expect, test } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

test("creates, persists, and clears a custom property icon", async ({ page }) => {
  const workspaceSlug = await loginAsDefault(page);
  const propertyName = `Icon ${Date.now().toString(36)}`;

  await page.goto(`/${workspaceSlug}/settings?tab=properties`, {
    waitUntil: "domcontentloaded",
  });
  await waitForPageText(page, "Properties");
  await page.getByRole("button", { name: "New property" }).click();
  const createDialog = page.getByRole("dialog", { name: "New property" });
  await expect(createDialog).toBeVisible();

  await createDialog.getByRole("button", { name: "Choose icon" }).click();
  await page.getByRole("button", { name: "Flag", exact: true }).click();
  await createDialog.getByLabel("Name").fill(propertyName);
  await createDialog.getByPlaceholder("Option name").fill("Critical");
  await createDialog.getByRole("button", { name: "Save property" }).click();

  await expect(createDialog).not.toBeVisible();
  const propertyNameCell = page.getByText(propertyName, { exact: true }).locator("..");
  await expect(propertyNameCell).toBeVisible();
  await expect(propertyNameCell.locator('[data-property-icon="flag"]')).toBeVisible();

  await page.getByRole("button", { name: `Actions for ${propertyName}` }).click();
  await page.getByRole("menuitem", { name: "Edit" }).click();
  const editDialog = page.getByRole("dialog", { name: "Edit property" });
  await expect(editDialog).toBeVisible();
  await expect(
    editDialog.locator('[data-property-icon="flag"]'),
  ).toBeVisible();

  await editDialog.getByRole("button", { name: "Choose icon" }).click();
  await page.getByRole("button", { name: "Remove icon" }).click();
  await editDialog.getByRole("button", { name: "Save property" }).click();
  await expect(editDialog).not.toBeVisible();
  await expect(propertyNameCell.locator("[data-property-icon]")).toHaveCount(0);
});
