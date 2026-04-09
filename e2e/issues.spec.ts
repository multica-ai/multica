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

  test("can create and edit issue schedule dates", async ({ page }) => {
    const title = "E2E Scheduled " + Date.now();
    const schedule = await page.evaluate(() => {
      const build = (offset: number) => {
        const date = new Date();
        date.setHours(0, 0, 0, 0);
        date.setDate(date.getDate() + offset);
        return {
          dataDay: date.toLocaleDateString(),
          iso: date.toISOString(),
          label: date.toLocaleDateString("en-US", { month: "short", day: "numeric" }),
        };
      };

      return {
        start: build(0),
        end: build(1),
        updatedEnd: build(2),
      };
    });

    await page.getByRole("button", { name: "New issue" }).click();
    await page.getByLabel("Issue title").fill(title);

    await page.getByRole("button", { name: "Start date" }).click();
    await page.locator(`[data-slot="popover-content"] [data-day="${schedule.start.dataDay}"]`).last().click();

    await page.getByRole("button", { name: "End date" }).click();
    await page.locator(`[data-slot="popover-content"] [data-day="${schedule.end.dataDay}"]`).last().click();

    await page.getByRole("button", { name: "Create Issue" }).click();

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(/\/issues\/[\w-]+/);
    const issueId = page.url().split("/").pop();
    if (!issueId) {
      throw new Error("Missing issue id from detail URL");
    }

    await expect(page.getByRole("button", { name: schedule.start.label })).toBeVisible();
    await expect(page.getByRole("button", { name: schedule.end.label })).toBeVisible();

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        start_date: issue.start_date,
        end_date: issue.end_date,
      };
    }).toEqual({
      start_date: schedule.start.iso,
      end_date: schedule.end.iso,
    });

    await page.getByRole("button", { name: schedule.start.label }).click();
    await page.getByRole("button", { name: "Clear date" }).click();
    await expect(page.getByRole("button", { name: "Start date" })).toBeVisible();

    await page.getByRole("button", { name: schedule.end.label }).click();
    await page.locator(`[data-slot="popover-content"] [data-day="${schedule.updatedEnd.dataDay}"]`).last().click();
    await expect(page.getByRole("button", { name: schedule.updatedEnd.label })).toBeVisible();

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        start_date: issue.start_date,
        end_date: issue.end_date,
      };
    }).toEqual({
      start_date: null,
      end_date: schedule.updatedEnd.iso,
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
