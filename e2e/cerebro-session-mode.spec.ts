import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Cerebro session mode", () => {
  let api: TestApiClient;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", true);
    await api.setWorkspaceFeatureFlag("cerebro_session_modes", true);
    const issue = await api.createIssue(`E2E Session Mode ${Date.now()}`);
    issueId = issue.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", false).catch(() => {});
    await api.setWorkspaceFeatureFlag("cerebro_session_modes", false).catch(() => {});
    await api.cleanup();
  });

  test("starts the first issue thread in the selected Plan mode and keeps it after reload", async ({
    page,
  }) => {
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.getByText("Properties")).toBeVisible();

    const newThreadMode = page.getByRole("combobox", { name: "New thread mode" });
    await expect(newThreadMode).toBeVisible();
    await expect(newThreadMode).toHaveText(/Build/);
    await newThreadMode.click();
    await page.getByRole("option", { name: "Plan" }).click();
    await expect(newThreadMode).toHaveText(/Plan/);

    const commentText = `Session mode comment ${Date.now()}`;
    const composer = page.getByTestId("composer-input").last();
    await composer.locator('[contenteditable="true"]').fill(commentText);
    await composer.getByRole("button", { name: "Submit" }).click();

    await expect(page.getByText(commentText).first()).toBeVisible({ timeout: 10000 });
    const sessionMode = page.getByRole("combobox", { name: "Session mode" }).last();
    await expect(sessionMode).toHaveText(/Plan/);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(commentText).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole("combobox", { name: "Session mode" }).last()).toHaveText(/Plan/);
  });
});
