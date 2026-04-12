import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Navigation", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("sidebar navigation works", async ({ page }) => {
    await page.getByRole("link", { name: "Board" }).click();
    await page.waitForURL("**/board");
    await expect(page).toHaveURL(/\/board/);

    await page.getByRole("link", { name: "Backlog" }).click();
    await page.waitForURL("**/backlog");
    await expect(page).toHaveURL(/\/backlog/);

    await page.getByRole("link", { name: "Today" }).click();
    await page.waitForURL("**/today");
    await expect(page).toHaveURL(/\/today/);

    await page.getByRole("link", { name: "Upcoming" }).click();
    await page.waitForURL("**/upcoming");
    await expect(page).toHaveURL(/\/upcoming/);

    await page.getByRole("link", { name: "My Work" }).click();
    await page.waitForURL("**/my-work");
    await expect(page).toHaveURL(/\/my-work/);

    await page.getByRole("link", { name: "Projects" }).click();
    await page.waitForURL("**/projects");
    await expect(page).toHaveURL(/\/projects/);

    await page.getByRole("link", { name: "Notifications" }).click();
    await page.waitForURL("**/notifications");
    await expect(page).toHaveURL(/\/notifications/);

    await page.getByRole("link", { name: "Agents" }).click();
    await page.waitForURL("**/agents");
    await expect(page).toHaveURL(/\/agents/);
  });

  test("settings page loads via sidebar", async ({ page }) => {
    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");

    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Profile" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Members" })).toBeVisible();
  });

  test("agents page shows agent list", async ({ page }) => {
    await page.getByRole("link", { name: "Agents" }).click();
    await page.waitForURL("**/agents");

    await expect(page.getByRole("heading", { name: "Agents" })).toBeVisible();
  });

  test("agent detail route opens the selected agent", async ({ page }, testInfo) => {
    const agent = await api.ensureAgent(`E2E Route Agent ${testInfo.parallelIndex}`);

    await page.goto(`/agents/${agent.id}`);
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page.getByRole("heading", { name: agent.name })).toBeVisible();
    await expect(page.getByText("Instructions").first()).toBeVisible();
  });

  test("inbox route keeps the issue in the address", async ({ page }) => {
    const issueTitle = "Inbox Route Test " + Date.now();
    const issue = await api.createIssue(issueTitle);
    await api.createInboxItem(issue.id, issueTitle);

    await page.goto(`/inbox?issue=${issue.id}`);
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/inbox\\?issue=${issue.id}$`));
    await expect(page.getByText(issueTitle).first()).toBeVisible();
    await expect(page.getByLabel("Leave a comment...")).toBeVisible();
  });

  test("mobile navigation opens from the top toolbar", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();

    await page.getByRole("button", { name: "Open navigation" }).click();
    await page.getByRole("link", { name: "Settings" }).click();
    await page.waitForURL("**/settings");

    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Profile" })).toBeVisible();
  });

  test("mobile notifications drills into detail and returns to list", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();

    const issueTitle = "Mobile Inbox Route " + Date.now();
    const issue = await api.createIssue(issueTitle);
    await api.createInboxItem(issue.id, issueTitle);

    await page.goto(`/inbox?issue=${issue.id}`);
    await expect(page).toHaveURL(new RegExp(`/inbox\\?issue=${issue.id}$`));
    await expect(page.getByRole("button", { name: "Back to Notifications" })).toBeVisible();

    await page.getByRole("button", { name: "Back to Notifications" }).click();
    await expect(page).toHaveURL(/\/inbox$/);
  });
});
