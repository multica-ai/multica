import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { Page } from "@playwright/test";
import type { TestApiClient } from "./fixtures";

test.describe("Issues", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  async function createVisibleIssue(page: Page, title: string) {
    const issue = await api.createIssue(title);
    await page.reload();
    await expect(page.getByText(title)).toBeVisible({ timeout: 10000 });
    return issue;
  }

  test("issues page loads with board view", async ({ page }) => {
    const title = "E2E Board " + Date.now();
    await createVisibleIssue(page, title);

    // Board columns should be visible
    await expect(page.getByText("Backlog").first()).toBeVisible();
    await expect(page.getByText("Todo").first()).toBeVisible();
    await expect(page.getByText("In Progress").first()).toBeVisible();
  });

  test("can switch between board and list view", async ({ page }) => {
    const title = "E2E View Switch " + Date.now();
    await createVisibleIssue(page, title);

    // Switch to list view
    await page.getByRole("button", { name: "Board view" }).click();
    await page.getByRole("menuitem", { name: "List" }).click();
    await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible();

    // Switch back to board view
    await page.getByRole("button", { name: "List view" }).click();
    await page.getByRole("menuitem", { name: "Board" }).click();
    await expect(page.getByText("Backlog").first()).toBeVisible();
  });

  test("can create a new issue", async ({ page }) => {
    await page.getByRole("button", { name: /New Issue/ }).first().click();

    const title = "E2E Created " + Date.now();
    await page.getByRole("textbox", { name: "Issue title" }).fill(title);
    await page.getByRole("button", { name: "Create Issue" }).click();

    // New issue should appear on the page
    await expect(page.getByText(title).first()).toBeVisible({
      timeout: 10000,
    });
  });

  test("can navigate to issue detail page", async ({ page }) => {
    // Create a known issue via API so the test controls its own fixture
    const issue = await api.createIssue("E2E Detail Test " + Date.now());

    // Reload to see the new issue
    await page.reload();
    await expect(page.getByText(issue.title)).toBeVisible({ timeout: 10000 });

    // Navigate to the issue detail
    const issueLink = page.locator(`a[href="/issues/${issue.id}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();

    await page.waitForURL(/\/issues\/[\w-]+/);

    // Should show Properties panel
    await expect(page.locator("text=Properties")).toBeVisible();
    // Should show breadcrumb link back to Issues
    await expect(
      page.locator("a", { hasText: "Issues" }).first(),
    ).toBeVisible();
  });

  test("can cancel issue creation", async ({ page }) => {
    await page.getByRole("button", { name: /New Issue/ }).first().click();

    const titleInput = page.getByRole("textbox", { name: "Issue title" });
    await expect(titleInput).toBeVisible();

    await page.keyboard.press("Escape");

    await expect(titleInput).not.toBeVisible();
    await expect(page.getByRole("button", { name: /New Issue/ }).first()).toBeVisible();
  });
});
