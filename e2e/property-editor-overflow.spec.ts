import { expect, test } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

const OPTION_COUNT = 30;

/**
 * MUL-7188: the property editor is centred by transform and had no height
 * bound, so a long option list pushed "Save property" below the viewport
 * where it could not be reached at all — the property became unsaveable.
 */
test("keeps the property editor saveable when the option list is long", async ({
  page,
}) => {
  const workspaceSlug = await loginAsDefault(page);
  const propertyName = `Overflow ${Date.now().toString(36)}`;

  await page.goto(`/${workspaceSlug}/settings?tab=properties`, {
    waitUntil: "domcontentloaded",
  });
  await waitForPageText(page, "Properties");
  await page.getByRole("button", { name: "New property" }).click();
  const dialog = page.getByRole("dialog", { name: "New property" });
  await expect(dialog).toBeVisible();

  await dialog.getByLabel("Name").fill(propertyName);
  const addOption = dialog.getByRole("button", { name: "Add option" });
  for (let i = 0; i < OPTION_COUNT - 1; i++) {
    await addOption.click();
  }
  await expect(dialog.getByPlaceholder("Option name")).toHaveCount(OPTION_COUNT);
  await dialog.getByPlaceholder("Option name").first().fill("Critical");

  const viewport = page.viewportSize();
  const box = await dialog.boundingBox();
  if (!viewport || !box) throw new Error("Could not measure the property editor");
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);

  // Clicking rather than asserting visibility: an off-screen footer is what
  // the report was about, and Playwright refuses to click what it cannot
  // bring into view.
  await dialog.getByRole("button", { name: "Save property" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText(propertyName, { exact: true })).toBeVisible();
});
