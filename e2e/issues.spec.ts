import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Issues", () => {
  let api: TestApiClient;
  let seedIssue: { id: string; title: string };

  const applyDateFromPopover = async (page: import("@playwright/test").Page, dataDay: string) => {
    const popover = page.locator('[data-slot="popover-content"]').last();
    await popover.locator(`[data-day="${dataDay}"]`).last().click();
    await popover.getByRole("button", { name: "Apply" }).click();
  };

  const normalizeIso = (value: string | null) => (value ? new Date(value).toISOString() : null);

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
    await expect(page.getByRole("link", { name: "Board" })).toBeVisible();
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

    await page.goto("/backlog");
    await expect(page.getByText(title).first()).toBeVisible({ timeout: 10000 });
  });

  test("backlog today and upcoming routes render derived issue views", async ({ page }) => {
    const today = new Date();
    today.setHours(12, 0, 0, 0);

    const tomorrow = new Date(today);
    tomorrow.setDate(today.getDate() + 1);

    const backlogTitle = "E2E Backlog View " + Date.now();
    const todayTitle = "E2E Today View " + Date.now();
    const upcomingTitle = "E2E Upcoming View " + Date.now();

    await api.createIssue(backlogTitle, { status: "backlog" });
    await api.createIssue(todayTitle, {
      status: "todo",
      due_date: today.toISOString(),
    });
    await api.createIssue(upcomingTitle, {
      status: "todo",
      start_date: tomorrow.toISOString(),
    });

    await page.reload();

    await page.goto("/backlog");
    await expect(page.getByText(backlogTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(todayTitle).first()).not.toBeVisible();

    await page.goto("/today");
    await expect(page.getByText(todayTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(upcomingTitle).first()).not.toBeVisible();

    await page.goto("/upcoming");
    await expect(page.getByText(upcomingTitle).first()).toBeVisible({ timeout: 10000 });
  });

  test("backlog view opens issue detail", async ({ page }) => {
    const title = "E2E Backlog Detail " + Date.now();
    const issue = await api.createIssue(title, { status: "backlog" });

    await page.reload();
    await page.goto("/backlog");

    const issueLink = page.getByRole("link", { name: new RegExp(title) }).first();
    await expect(issueLink).toBeVisible({ timeout: 10000 });
    await issueLink.click();

    await page.waitForURL(new RegExp(`/issues/${issue.id}$`));
    await expect(page.getByText("Properties")).toBeVisible();
  });

  test("project board shows only issues linked to that project", async ({ page }) => {
    const project = await api.createProject({
      title: "E2E Project Board " + Date.now(),
      icon: "📁",
    });

    const linkedTitle = "E2E Project Board Linked " + Date.now();
    const unrelatedTitle = "E2E Project Board Other " + Date.now();

    await api.createIssue(linkedTitle, {
      status: "todo",
      project_id: project.id,
    });
    await api.createIssue(unrelatedTitle, {
      status: "todo",
    });

    await page.reload();
    await page.goto(`/projects/${project.id}/board`);

    await expect(page.getByText(linkedTitle).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText(unrelatedTitle).first()).not.toBeVisible();
    await expect(page.getByText(`${project.title} Board`).first()).toBeVisible();
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
    await applyDateFromPopover(page, schedule.start.dataDay);

    await page.getByRole("button", { name: "End date" }).click();
    await applyDateFromPopover(page, schedule.end.dataDay);

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
        start_date: normalizeIso(issue.start_date),
        end_date: normalizeIso(issue.end_date),
      };
    }).toEqual({
      start_date: schedule.start.iso,
      end_date: schedule.end.iso,
    });

    await page.getByRole("button", { name: schedule.start.label }).click();
    await page.getByRole("button", { name: "Clear date" }).click();
    await expect(page.getByRole("button", { name: "Start date" })).toBeVisible();

    await page.getByRole("button", { name: schedule.end.label }).click();
    await applyDateFromPopover(page, schedule.updatedEnd.dataDay);
    await expect(page.getByRole("button", { name: schedule.updatedEnd.label })).toBeVisible();

    await expect.poll(async () => {
      const issue = await api.getIssue(issueId);
      return {
        start_date: normalizeIso(issue.start_date),
        end_date: normalizeIso(issue.end_date),
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
