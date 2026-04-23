import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

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
    await page.reload();

    // Board columns should be visible
    await expect(page.locator("text=Backlog")).toBeVisible();
    await expect(page.locator("text=Todo")).toBeVisible();
    await expect(page.locator("text=In Progress")).toBeVisible();
  });

  test("can switch from board to list view", async ({ page }) => {
    const title = "E2E List Switch " + Date.now();
    await api.createIssue(title);
    await page.reload();
    await expect(page.locator("text=Backlog")).toBeVisible();

    // Switch to list view
    await page.click("text=List");
    await expect(page.getByText(title)).toBeVisible();
  });

  test("can create a new issue", async ({ page }) => {
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
    const issue = await api.createIssue("E2E Detail Test " + Date.now());
    await page.reload();

    await page.getByText(issue.title).first().click();
    await page.waitForURL(new RegExp(`\\?peek=${issue.id}$`));
    await page.locator('button[title="Open full detail"]').first().click();
    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));

    await expect(page.locator("text=Properties")).toBeVisible();
    await expect(
      page.locator("a", { hasText: "Issues" }).first(),
    ).toBeVisible();
  });

  test("can dismiss issue creation", async ({ page }) => {
    await page.getByRole("button", { name: "New Issue" }).click();

    const titleInput = page.getByRole("textbox", { name: "Issue title" });
    await expect(titleInput).toBeVisible();

    await page.keyboard.press("Escape");

    await expect(titleInput).not.toBeVisible();
    await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible();
  });

  test("clicking an issue card opens preview and expand opens full detail", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const title = "E2E Preview Workbench " + Date.now();
    const issue = await api.createIssue(title);

    await page.reload();
    await page.getByText(title).first().click();

    await page.waitForURL(new RegExp(`\\?peek=${issue.id}$`));
    await expect(page.locator('button[title="Open full detail"]').first()).toBeVisible();
    await page.locator('button[title="Open full detail"]').first().click();
    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));
    await expect(
      page.getByRole("button", { name: "Back to Workbench" }),
    ).toBeHidden();
  });

  test("preview shows queued execution details for a seeded task", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const title = "E2E Queued Preview " + Date.now();
    const issue = await api.createIssue(title);
    await api.seedIssueExecution(issue.id, {
      status: "queued",
      triggerText: "Please investigate queued preview",
      priority: 3,
    });

    await page.reload();
    await page.getByText(title).first().click();

    await page.waitForURL(new RegExp(`\\?peek=${issue.id}$`));
    await expect(page.getByText("Queued for execution").first()).toBeVisible();
    await expect(
      page.getByText("Trigger: Please investigate queued preview").first(),
    ).toBeVisible();
  });
});
