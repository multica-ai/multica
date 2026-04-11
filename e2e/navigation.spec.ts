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
    await page.getByRole("link", { name: "Inbox" }).click();
    await page.waitForURL("**/inbox");
    await expect(page).toHaveURL(/\/inbox/);

    await page.getByRole("link", { name: "Agents" }).click();
    await page.waitForURL("**/agents");
    await expect(page).toHaveURL(/\/agents/);

    await page.getByRole("link", { name: "Issues", exact: true }).click();
    await page.waitForURL("**/issues");
    await expect(page).toHaveURL(/\/issues/);
  });

  test("projects page loads from the sidebar", async ({ page }) => {
    await page.getByRole("link", { name: "Projects" }).click();
    await page.waitForURL("**/projects");

    await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
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

  test("mobile inbox drills into detail and returns to list", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();

    const issueTitle = "Mobile Inbox Route " + Date.now();
    const issue = await api.createIssue(issueTitle);
    await api.createInboxItem(issue.id, issueTitle);

    await page.goto(`/inbox?issue=${issue.id}`);
    await expect(page).toHaveURL(new RegExp(`/inbox\\?issue=${issue.id}$`));
    await expect(page.getByRole("button", { name: "Back to Inbox" })).toBeVisible();

    await page.getByRole("button", { name: "Back to Inbox" }).click();
    await expect(page).toHaveURL(/\/inbox$/);
  });
});
