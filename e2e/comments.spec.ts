import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.createIssue("E2E Comment Test " + Date.now());
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("can add a comment on an issue", async ({ page }) => {
    // Wait for issues to load and click first one. `*=` matches both legacy
    // `/issues/{id}` and URL-refactored `/{slug}/issues/{id}` hrefs.
    const issueLink = page.locator('a[href*="/issues/"]').first();
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    // Wait for issue detail to load
    await expect(page.locator("text=Properties")).toBeVisible();

    // Type a comment
    const commentText = "E2E comment " + Date.now();
    const commentInput = page.locator(
      'input[placeholder="Leave a comment..."]',
    );
    await commentInput.fill(commentText);

    // Submit the comment
    await page.locator('form button[type="submit"]').last().click();

    // Comment should appear in the activity section
    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    const issueLink = page.locator('a[href*="/issues/"]').first();
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    await expect(page.locator("text=Properties")).toBeVisible();

    // Submit button should be disabled when input is empty
    const submitBtn = page.locator('form button[type="submit"]').last();
    await expect(submitBtn).toBeDisabled();
  });
});
