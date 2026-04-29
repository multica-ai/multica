import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const issue = await api.createIssue("E2E Comment Test " + Date.now());
    issueId = issue.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("can add a comment on an issue", async ({ page }) => {
    await page.goto(`/e2e-workspace/issues/${issueId}`);
    await page.waitForURL(new RegExp(`/issues/${issueId}$`));

    await expect(page.locator("text=Properties")).toBeVisible();

    const commentText = "E2E comment " + Date.now();
    const commentEditor = page.locator(".rich-text-editor").last();
    const composer = commentEditor.locator(
      'xpath=ancestor::div[contains(@class, "ring-1")][1]',
    );
    await commentEditor.focus();
    await page.keyboard.type(commentText);
    const submitBtn = composer.locator('button[data-slot="button"]').last();
    await expect(submitBtn).toBeEnabled();
    await submitBtn.click({ force: true });

    await expect(page.getByText(commentText).last()).toBeVisible({
      timeout: 10000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await page.goto(`/e2e-workspace/issues/${issueId}`);
    await page.waitForURL(new RegExp(`/issues/${issueId}$`));

    await expect(page.locator("text=Properties")).toBeVisible();

    const commentEditor = page.locator(".rich-text-editor").last();
    const composer = commentEditor.locator(
      'xpath=ancestor::div[contains(@class, "ring-1")][1]',
    );
    const submitBtn = composer.locator('button[data-slot="button"]').last();
    await expect(submitBtn).toBeDisabled();
  });
});
