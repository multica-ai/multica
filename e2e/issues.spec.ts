import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Issues", () => {
  let api: TestApiClient;
  let seedIssue: { id: string; title: string };

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    seedIssue = await api.createIssue("E2E Seed Issue " + Date.now());
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("issues page loads with board view", async ({ page }) => {
    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByRole("link", { name: "Issues", exact: true })).toBeVisible();
    await expect(page.getByText("Backlog").first()).toBeVisible();
    await expect(page.getByText("Todo").first()).toBeVisible();
    await expect(page.getByText("In Progress").first()).toBeVisible();
  });

  test("can switch between board and list view", async ({ page }) => {
    await expect(page.getByText("Backlog").first()).toBeVisible();

    await page.getByRole("button", { name: "View options" }).click();
    await page.getByRole("menuitem", { name: "List" }).click();
    await expect(page.getByText(seedIssue.title).first()).toBeVisible();

    await page.getByRole("button", { name: "View options" }).click();
    await page.getByRole("menuitem", { name: "Board" }).click();
    await expect(page.getByText("Backlog").first()).toBeVisible();
  });

  test("can create a new issue", async ({ page }) => {
    await page.getByRole("button", { name: "New issue" }).click();

    const title = "E2E Created " + Date.now();
    await page.getByLabel("Issue title").fill(title);
    await page.getByRole("button", { name: "Create Issue" }).click();

    await expect(page.locator(`text=${title}`).first()).toBeVisible({
      timeout: 10000,
    });
  });

  test("can navigate to issue detail page", async ({ page }) => {
    const title = "E2E Detail Test " + Date.now();
    const issue = await api.createIssue(title);

    await page.reload();
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByText("Properties")).toBeVisible();
    await expect(page.getByRole("button", { name: "Post comment" })).toBeVisible();
  });

  test("can cancel issue creation", async ({ page }) => {
    await page.getByRole("button", { name: "New issue" }).click();
    await expect(page.getByLabel("Issue title")).toBeVisible();
    await page.getByRole("button", { name: "Close new issue dialog" }).click();
    await expect(page.getByLabel("Issue title")).not.toBeVisible();
    await expect(page.getByRole("button", { name: "New issue" })).toBeVisible();
  });

  test("board route keeps the board page", async ({ page }) => {
    await page.goto("/board");
    await page.waitForURL("**/board");

    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByText("Backlog").first()).toBeVisible();
    await expect(page.getByRole("button", { name: "View options" })).toHaveCount(
      0,
    );
  });
});
