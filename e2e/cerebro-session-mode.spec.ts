import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Cerebro session mode", () => {
  let api: TestApiClient;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", true);
    const issue = await api.createIssue(`E2E Session Mode ${Date.now()}`);
    issueId = issue.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", false).catch(() => {});
    await api.cleanup();
  });

  test("can switch a comment thread session into Plan mode and keep it after reload", async ({
    page,
  }) => {
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.getByText("Properties")).toBeVisible();

    const commentText = `Session mode comment ${Date.now()}`;
    await api.createComment(issueId, commentText);
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(commentText)).toBeVisible({ timeout: 10000 });

    const modeSelect = page.getByRole("combobox", { name: "Session mode" }).last();
    await expect(modeSelect).toBeVisible();
    await expect(modeSelect).toHaveText(/Build/);
    await modeSelect.click();
    await page.getByRole("option", { name: "Plan" }).click();
    await expect(modeSelect).toHaveText(/Plan/);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(commentText)).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole("combobox", { name: "Session mode" }).last()).toHaveText(/Plan/);
  });
});
