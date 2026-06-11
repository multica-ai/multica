import { test, expect } from "@playwright/test";
import { createTestApi, gotoAppPage, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let issue: { id: string };
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    issue = await api.createIssue("E2E Comment Test " + Date.now());
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("can add a comment on an issue", async ({ page }) => {
    await gotoAppPage(page, `/${workspaceSlug}/issues/${issue.id}`);
    await expect(page).toHaveURL(/\/issues\/[\w-]+/);

    // Wait for issue detail to load
    await expect(page.locator("text=Properties")).toBeVisible({
      timeout: 15000,
    });

    // Type a comment
    const commentText = "E2E comment " + Date.now();
    const composer = page.locator("div.ring-border:has(.rich-text-editor)").last();
    const commentInput = composer.locator(".rich-text-editor");
    await commentInput.fill(commentText);

    // Submit the comment
    await composer.getByRole("button").last().click();

    // Comment should appear in the activity section
    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await gotoAppPage(page, `/${workspaceSlug}/issues/${issue.id}`);
    await expect(page).toHaveURL(/\/issues\/[\w-]+/);

    await expect(page.locator("text=Properties")).toBeVisible({
      timeout: 15000,
    });

    const composer = page.locator("div.ring-border:has(.rich-text-editor)").last();
    const submitBtn = composer.getByRole("button").last();
    await expect(submitBtn).toBeDisabled();
  });
});
