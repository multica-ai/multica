import { test, expect } from "@playwright/test";
import pg from "pg";
import { loginAsDefault, createTestApi, preferManualCreateMode, reloadAppPage } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function setIssueTimestamps(
  issueId: string,
  timestamps: { createdAt: Date; updatedAt?: Date },
) {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    await client.query(
      `
        UPDATE issue
        SET created_at = $2, updated_at = $3
        WHERE id = $1
      `,
      [
        issueId,
        timestamps.createdAt.toISOString(),
        (timestamps.updatedAt ?? timestamps.createdAt).toISOString(),
      ],
    );
  } finally {
    await client.end();
  }
}

test.describe("Issues", () => {
  let api: TestApiClient;

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
    await reloadAppPage(page);

    // Board columns should be visible
    await expect(page.locator("text=Backlog")).toBeVisible();
    await expect(page.locator("text=Todo")).toBeVisible();
    await expect(page.locator("text=In Progress")).toBeVisible();
  });

  test("can switch from board to list view", async ({ page }) => {
    const title = "E2E List Switch " + Date.now();
    await api.createIssue(title);
    await reloadAppPage(page);
    await expect(page.locator("text=Backlog")).toBeVisible();

    // Switch to list view
    await page.click("text=List");
    await expect(page.getByText(title)).toBeVisible();
  });

  test("can filter issues by created and updated dates", async ({ page }) => {
    const suffix = Date.now();
    const todayTitle = `E2E Date Today ${suffix}`;
    const oldTitle = `E2E Date Old ${suffix}`;
    const updatedTodayTitle = `E2E Date Updated Today ${suffix}`;
    await api.createIssue(todayTitle);
    const oldIssue = await api.createIssue(oldTitle);
    const updatedTodayIssue = await api.createIssue(updatedTodayTitle);
    const oldDate = new Date();
    oldDate.setDate(oldDate.getDate() - 8);
    await setIssueTimestamps(oldIssue.id, { createdAt: oldDate });
    await setIssueTimestamps(updatedTodayIssue.id, {
      createdAt: oldDate,
      updatedAt: new Date(),
    });

    await reloadAppPage(page);
    await expect(page.getByText(todayTitle)).toBeVisible();
    await expect(page.getByText(oldTitle)).toBeVisible();
    await expect(page.getByText(updatedTodayTitle)).toBeVisible();

    await page.getByRole("button", { name: /filter/i }).click();
    await page.getByRole("menuitem", { name: /^Date\b/ }).hover();
    await page.getByRole("menuitem", { name: "Today" }).click();

    await expect(page.getByRole("button", { name: /1 filter/i })).toBeVisible();
    await expect(page.getByText(todayTitle)).toBeVisible();
    await expect(page.getByText(oldTitle)).toBeHidden({ timeout: 10000 });
    await expect(page.getByText(updatedTodayTitle)).toBeHidden({ timeout: 10000 });

    await page.getByRole("button", { name: /1 filter/i }).click();
    const dateFilterItem = page.getByRole("menuitem", { name: /^Date\b/ });
    await dateFilterItem.focus();
    await page.keyboard.press("ArrowRight");
    const updatedDateField = page.getByRole("menuitemradio", { name: "Updated" });
    await expect(updatedDateField).toBeVisible();
    await updatedDateField.press("Enter");
    await expect(page.getByText(todayTitle)).toBeVisible();
    await expect(page.getByText(updatedTodayTitle)).toBeVisible();
    await expect(page.getByText(oldTitle)).toBeHidden({ timeout: 10000 });
  });

  test("can filter issues by custom created date", async ({ page }) => {
    const suffix = Date.now();
    const todayTitle = `E2E Date Custom Today ${suffix}`;
    const oldTitle = `E2E Date Custom Old ${suffix}`;
    await api.createIssue(todayTitle);
    const oldIssue = await api.createIssue(oldTitle);
    const oldDate = new Date();
    oldDate.setDate(oldDate.getDate() - 8);
    await setIssueTimestamps(oldIssue.id, { createdAt: oldDate });

    await reloadAppPage(page);
    await expect(page.getByText(todayTitle)).toBeVisible();
    await expect(page.getByText(oldTitle)).toBeVisible();

    await page.getByRole("button", { name: /filter/i }).click();
    await page.getByRole("menuitem", { name: /^Date\b/ }).hover();
    const customDateButton = page.getByRole("button", { name: "Custom date or range" });
    await expect(customDateButton).toBeVisible();
    await customDateButton.click();
    const todayDataDay = await page.evaluate(() => new Date().toLocaleDateString());
    await page.locator(`[data-day="${todayDataDay}"]`).click();
    await page.getByRole("button", { name: "Apply" }).click();
    await expect(page.getByText(todayTitle)).toBeVisible();
    await expect(page.getByText(oldTitle)).toBeHidden({ timeout: 10000 });
  });

  test("can create a new issue", async ({ page }) => {
    await preferManualCreateMode(page);

    const newIssueButton = page.getByRole("button", { name: "New Issue" });
    await expect(newIssueButton).toBeVisible();
    await newIssueButton.click();

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
    await reloadAppPage(page);

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
    await preferManualCreateMode(page);

    await page.getByRole("button", { name: "New Issue" }).click();

    const titleInput = page.getByRole("textbox", { name: "Issue title" });
    await expect(titleInput).toBeVisible();

    await page.keyboard.press("Escape");

    await expect(titleInput).not.toBeVisible();
    await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible();
  });
});
