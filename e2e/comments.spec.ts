import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let issue: { id: string };

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    issue = await api.createIssue("E2E Comment Test " + Date.now());
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can add a comment on an issue", async ({ page }) => {
    await page.goto(`/issues/${issue.id}`);
    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByText("Properties")).toBeVisible();

    const commentText = "E2E comment " + Date.now();
    const commentInput = page.getByLabel("Leave a comment...");
    await commentInput.fill(commentText);

    await page.getByRole("button", { name: "Post comment" }).click();

    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await page.goto(`/issues/${issue.id}`);
    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByText("Properties")).toBeVisible();

    const submitBtn = page.getByRole("button", { name: "Post comment" });
    await expect(submitBtn).toBeDisabled();
  });
});
