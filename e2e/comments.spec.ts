import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let issueId: string;
  let issueTitle: string;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    issueTitle = "E2E Comment Test " + Date.now();
    const issue = await api.createIssue(issueTitle);
    issueId = issue.id;
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can add a comment on an issue", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, issueTitle);

    // Wait for issue detail to load
    await expect(page.locator("text=Properties")).toBeVisible();

    // Type a comment
    const commentText = "E2E comment " + Date.now();
    const editor = page
      .locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])')
      .first();
    await expect(editor).toBeVisible();
    await editor.click({ force: true });
    await editor.fill(commentText);

    await page.keyboard.press("ControlOrMeta+Enter");

    // Comment should appear in the activity section
    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, issueTitle);

    await expect(page.locator("text=Properties")).toBeVisible();

    // Submit button should be disabled when input is empty
    const editor = page
      .locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])')
      .first();
    await expect(editor).toBeVisible();
    const composer = editor.locator("xpath=ancestor::div[contains(@class, 'rounded-lg')][1]");
    const submitBtn = composer.locator("button:has(svg.lucide-arrow-up)").last();
    await expect(submitBtn).toBeDisabled();
  });
});
