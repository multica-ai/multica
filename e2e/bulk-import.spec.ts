import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Bulk Issue Import", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can bulk import issues via plain text", async ({ page }) => {
    const suffix = Date.now();
    const titles = [
      `E2E Bulk A ${suffix}`,
      `E2E Bulk B ${suffix}`,
      `E2E Bulk C ${suffix}`,
    ];

    await page.waitForURL(/\/issues/);

    await page.getByRole("button", { name: "Import issues" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    await dialog.getByRole("textbox").fill(titles.join("\n"));

    await dialog.getByRole("button", { name: /Import 3/i }).click();

    await expect(dialog).not.toBeVisible({ timeout: 10000 });

    for (const title of titles) {
      await expect(page.getByText(title).first()).toBeVisible({ timeout: 10000 });
    }
  });

  test("bulk import button is disabled for empty text input", async ({ page }) => {
    await page.waitForURL(/\/issues/);

    await page.getByRole("button", { name: "Import issues" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    await expect(dialog.getByRole("button", { name: /^import$/i })).toBeDisabled();
  });
});
