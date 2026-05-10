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
    // Wait for issues to load and click first one. `*=` matches both legacy
    // `/issues/{id}` and URL-refactored `/{slug}/issues/{id}` hrefs.
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    // Wait for issue detail to load
    await expect(page.locator("text=Properties")).toBeVisible();

    // Type a comment
    const commentText = "E2E comment " + Date.now();
    const commentBox = page.getByTestId("comment-input");
    const commentInput = commentBox.locator('[contenteditable="true"]');
    await expect(commentInput).toBeVisible();
    await commentInput.click();
    await page.keyboard.type(commentText);

    // Submit the comment
    const submitComment = commentBox.getByRole("button", { name: "Submit comment" });
    await expect(submitComment).toBeEnabled();
    await submitComment.evaluate((button: HTMLButtonElement) => button.click());

    // Comment should appear in the activity section
    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    await expect(page.locator("text=Properties")).toBeVisible();

    // Submit button should be disabled when input is empty
    const commentBox = page.getByTestId("comment-input");
    const commentInput = commentBox.locator('[contenteditable="true"]');
    await expect(commentInput).toBeVisible();
    const submitBtn = commentBox.getByRole("button", { name: "Submit comment" });
    await expect(submitBtn).toBeDisabled();
  });
});
