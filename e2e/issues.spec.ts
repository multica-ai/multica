import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Issues", () => {
  let api: TestApiClient;

  async function openNewIssue(page: Page) {
    await page.getByRole("button", { name: "New Issue" }).first().click({ force: true });
    const switchToManual = page.getByRole("button", { name: "Switch to Manual" });
    if (await switchToManual.waitFor({ state: "visible", timeout: 1500 }).then(() => true, () => false)) {
      await switchToManual.click();
    }
  }

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("issues page loads with board view", async ({ page }) => {
    await api.createIssue("E2E Board View " + Date.now());
    await page.reload();

    // Board defaults to the active/current slice; backlog/done stay available
    // through column filters but are not rendered by default.
    await expect(page.locator("text=Todo")).toBeVisible();
    await expect(page.locator("text=In Progress")).toBeVisible();
    await expect(page.locator("text=In Review")).toBeVisible();
    await expect(page.locator("text=Blocked")).toBeVisible();
    await expect(page.locator("text=Hidden columns")).toBeVisible();
  });

  test("can switch from board to list view", async ({ page }) => {
    const title = "E2E List Switch " + Date.now();
    await api.createIssue(title);
    await page.reload();
    await expect(page.locator("text=Todo")).toBeVisible();

    // Switch to list view through the view-mode menu. Avoid `text=List`,
    // which can match issue titles.
    await page.getByRole("button", { name: "Board view" }).click();
    await page.getByRole("menuitem", { name: "List" }).click();
    await expect(page.getByText(title)).toBeVisible();
  });

  test("can create a new issue", async ({ page }) => {
    await openNewIssue(page);

    const title = "E2E Created " + Date.now();
    const titleInput = page.getByRole("textbox", { name: "Issue title" });
    await expect(titleInput).toBeVisible();
    await titleInput.fill(title);
    await page.getByRole("button", { name: "Create Issue" }).click();

    await expect(page.getByText("Issue created")).toBeVisible({ timeout: 10000 });
    await expect(
      page.getByRole("region", { name: /Notifications/ }).getByText(title),
    ).toBeVisible();

    await page.getByRole("button", { name: "View issue" }).click();
    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.locator("text=Properties")).toBeVisible();
  });

  test("can navigate to issue detail page", async ({ page }) => {
    // Create a known issue via API so the test controls its own fixture
    const issue = await api.createIssue("E2E Detail Test " + Date.now());

    // Reload to see the new issue
    await page.reload();

    // Navigate to the issue detail. Use a suffix match so the selector works
    // whether the href is legacy `/issues/{id}` or URL-refactored
    // `/{slug}/issues/{id}`.
    const issueLink = page.locator(`a[href$="/issues/${issue.id}"]`);
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

  test("can dismiss issue creation", async ({ page }) => {
    await openNewIssue(page);

    const titleInput = page.getByRole("textbox", { name: "Issue title" });
    await expect(titleInput).toBeVisible();

    await page.keyboard.press("Escape");

    await expect(titleInput).not.toBeVisible();
    await expect(page.getByRole("button", { name: "New Issue" }).first()).toBeVisible();
  });
});
