import { test, expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { loginAsDefault, createTestApi } from "./helpers";

/**
 * E2E coverage for the notifications routing feature:
 *  - Sidebar NOTIFICATIONS link with count + Clear
 *  - Notifications page rendering, filter bar, mark-read, dismiss, clear-all
 *  - Settings notifications tab — routing choice persists
 *
 * Inbox items are seeded via direct SQL (api.insertInboxItem) so each test has
 * deterministic state. The natural notification listener flow needs two users
 * acting on shared issues; that's covered by the Go listener tests instead.
 */

// Each test resets DB state in beforeEach. Serial mode keeps order
// predictable; retries cover transient hydration timing on the dev stack.
test.describe.configure({ mode: "serial", retries: 2 });

let api: TestApiClient;

test.beforeEach(async () => {
  api = await createTestApi();
  // Wipe any leftover inbox_item rows so per-test count assertions are
  // immune to leakage from previous runs.
  await api.resetInboxItems();
});

test.afterEach(async () => {
  await api.cleanup();
});

async function seedThreeNotifications(api: TestApiClient, issueId: string) {
  await api.insertInboxItem({
    type: "new_comment",
    title: "First comment",
    issueId,
    details: { comment_id: "fake-comment" },
  });
  await api.insertInboxItem({
    type: "status_changed",
    title: "Status moved",
    issueId,
    details: { from: "todo", to: "in_progress" },
  });
  await api.insertInboxItem({
    type: "reaction_added",
    title: "Reaction on your work",
    issueId,
    details: { emoji: "👍" },
  });
}

async function gotoNotificationsPage(page: Page, slug: string) {
  await page.goto(`/${slug}/notifications`);
  await page.waitForURL("**/notifications", { timeout: 15000 });
  // Wait for the data fetch to settle so per-row count assertions don't race
  // a still-loading TanStack query.
  await page.waitForLoadState("networkidle", { timeout: 15000 });
}

test.describe("Sidebar — NOTIFICATIONS link", () => {
  test("shows count when items exist and navigates to the page on click", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Sidebar link target");
    await seedThreeNotifications(api, issue.id);

    // Trigger a refetch to surface the count.
    await page.reload();

    const link = page.getByTestId("sidebar-notifications-link");
    await expect(link).toBeVisible();
    await expect(page.getByTestId("sidebar-notifications-count")).toHaveText("3");

    await link.click();
    await page.waitForURL(`**/${slug}/notifications`);
    await expect(page.getByTestId("notifications-count")).toHaveText("3");
  });

  test("Clear button archives all without navigating", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Sidebar clear target");
    await seedThreeNotifications(api, issue.id);

    await page.reload();
    await expect(page.getByTestId("sidebar-notifications-count")).toHaveText("3");

    // The Clear button is hover-revealed — force-click since we don't need
    // visual hover for the action to fire.
    const clearBtn = page.getByTestId("sidebar-notifications-clear");
    await clearBtn.click({ force: true });

    // After Clear: badge disappears (count goes to 0), and we're still on /issues.
    await expect(page.getByTestId("sidebar-notifications-count")).toBeHidden();
    expect(page.url()).toContain(`/${slug}/issues`);
  });
});

test.describe("Notifications page", () => {
  test("renders rows, marks read, and dismisses", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Notifications page target");
    await seedThreeNotifications(api, issue.id);

    await gotoNotificationsPage(page, slug);

    const rows = page.getByTestId("notification-row");
    await expect(rows).toHaveCount(3);

    // All rows start unread.
    const unreadCount = await rows.evaluateAll((els) =>
      els.filter((el) => el.getAttribute("data-read") === "false").length,
    );
    expect(unreadCount).toBe(3);

    // Mark all read.
    await page.getByRole("button", { name: "Mark all read" }).click();
    await expect.poll(
      async () =>
        rows.evaluateAll((els) =>
          els.filter((el) => el.getAttribute("data-read") === "true").length,
        ),
      { timeout: 5000 },
    ).toBe(3);

    // Dismiss one row via its row-hover button. Force the click since the
    // dismiss control is opacity-0 until hover and Playwright's .hover() is
    // not 100% reliable for triggering the CSS :hover transition in time.
    const firstRow = rows.first();
    await firstRow.getByTitle("Dismiss").click({ force: true });

    await expect(rows).toHaveCount(2);
  });

  test("clicking a row navigates to the linked issue", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Navigation target");
    await api.insertInboxItem({
      type: "new_comment",
      title: "Click me to navigate",
      issueId: issue.id,
      details: {},
    });

    await gotoNotificationsPage(page, slug);
    const row = page.getByTestId("notification-row").first();
    await expect(row).toBeVisible();

    await row.click();
    await page.waitForURL(`**/${slug}/issues/${issue.id}`);
  });

  test("Clear archives every visible item and shows the empty state", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Clear target");
    await seedThreeNotifications(api, issue.id);

    await gotoNotificationsPage(page, slug);
    await expect(page.getByTestId("notification-row")).toHaveCount(3);

    await page.getByTestId("notifications-clear-all").click();

    await expect(page.getByTestId("notification-row")).toHaveCount(0);
    await expect(page.getByText("You're all caught up.")).toBeVisible();
  });
});

test.describe("Notifications page — filter bar", () => {
  test("each filter shows only its category", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Filter target");

    // Seed one of every category that has its own filter.
    await api.insertInboxItem({
      type: "mentioned",
      title: "You were @-mentioned",
      issueId: issue.id,
      details: {},
    });
    await api.insertInboxItem({
      type: "new_comment",
      title: "New comment landed",
      issueId: issue.id,
      details: {},
    });
    await api.insertInboxItem({
      type: "status_changed",
      title: "Status changed",
      issueId: issue.id,
      details: { from: "todo", to: "in_progress" },
    });
    await api.insertInboxItem({
      type: "reaction_added",
      title: "Reaction added",
      issueId: issue.id,
      details: { emoji: "👍" },
    });

    await gotoNotificationsPage(page, slug);
    const rows = page.getByTestId("notification-row");
    await expect(rows).toHaveCount(4);

    // Mentions
    await page.getByTestId("notifications-filter-mentions").click();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toHaveAttribute("data-notification-type", "mentioned");

    // Comments
    await page.getByTestId("notifications-filter-comments").click();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toHaveAttribute("data-notification-type", "new_comment");

    // Status & priority
    await page.getByTestId("notifications-filter-status_priority").click();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toHaveAttribute("data-notification-type", "status_changed");

    // Reactions
    await page.getByTestId("notifications-filter-reactions").click();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toHaveAttribute("data-notification-type", "reaction_added");

    // Back to All
    await page.getByTestId("notifications-filter-all").click();
    await expect(rows).toHaveCount(4);
  });

  test("Unread filter narrows to read=false rows", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Unread filter target");
    await api.insertInboxItem({
      type: "new_comment",
      title: "Unread one",
      issueId: issue.id,
      details: {},
      read: false,
    });
    await api.insertInboxItem({
      type: "new_comment",
      title: "Already read",
      issueId: issue.id,
      details: {},
      read: true,
    });

    await gotoNotificationsPage(page, slug);
    await expect(page.getByTestId("notification-row")).toHaveCount(2);

    await page.getByTestId("notifications-filter-unread").click();
    const rows = page.getByTestId("notification-row");
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toHaveAttribute("data-read", "false");
  });

  test("filter with no matches shows the filtered empty state", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Empty filter target");
    await api.insertInboxItem({
      type: "new_comment",
      title: "Only a comment",
      issueId: issue.id,
      details: {},
    });

    await gotoNotificationsPage(page, slug);
    await page.getByTestId("notifications-filter-mentions").click();

    await expect(page.getByText("No notifications match this filter.")).toBeVisible();
    await expect(page.getByTestId("notification-row")).toHaveCount(0);

    // The fallback "Show all" link clears the filter.
    await page.getByRole("button", { name: "Show all" }).click();
    await expect(page.getByTestId("notification-row")).toHaveCount(1);
  });
});

test.describe("Realtime updates", () => {
  test("a notification arriving via WS updates the page without reload", async ({ page }) => {
    const slug = await loginAsDefault(page);

    // Primary user creates an issue assigned to themselves so they auto-
    // subscribe to it.
    const issue = await api.createIssue("Realtime target", {
      assignee_type: "member",
      assignee_id: api.getUserId(),
    });

    // Open the notifications page BEFORE the event fires — page must update
    // via the WS subscription, not by polling.
    await gotoNotificationsPage(page, slug);
    await expect(page.getByText("You're all caught up.")).toBeVisible();

    // Secondary workspace member triggers a status change on the issue.
    // This dispatches the bus event → notification_listeners create the
    // inbox item → WS pushes inbox:new to the primary user's browser.
    const secondary = await api.loginSecondaryUser(
      "e2e-secondary@multica.ai",
      "E2E Secondary",
    );
    const updateRes = await secondary.fetch(`/api/issues/${issue.id}`, {
      method: "PUT",
      body: JSON.stringify({
        title: issue.title,
        description: issue.description ?? "",
        status: "in_progress",
        priority: issue.priority,
      }),
    });
    if (!updateRes.ok) {
      throw new Error(`status change failed: ${updateRes.status} ${await updateRes.text()}`);
    }

    // The notification should appear without page reload, within a few
    // seconds of the WS event firing.
    await expect(page.getByTestId("notification-row")).toHaveCount(1, { timeout: 10000 });
    await expect(page.getByTestId("notification-row").first()).toHaveAttribute(
      "data-notification-type",
      "status_changed",
    );
  });
});

test.describe("Settings — Notifications tab", () => {
  test("changing a route choice persists across reload", async ({ page }) => {
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/settings`);
    await page.waitForURL("**/settings");

    // Open the Notifications tab. Use text= for resilience — base-ui's tab
    // role attribute can vary across versions.
    await page.locator("button", { hasText: "Notifications" }).first().click();
    await expect(
      page.getByRole("heading", { name: "Notifications", level: 2 }),
    ).toBeVisible();

    // Find the "Reaction on your content" row and switch it from
    // Notifications (default) to Off.
    const reactionRow = page
      .locator("text=Reaction on your content")
      .locator("xpath=ancestor::div[contains(@class,'flex')][1]");
    await reactionRow.locator("button", { hasText: "Off" }).click();

    // Reload and confirm the choice survived.
    await page.reload();
    await page.locator("button", { hasText: "Notifications" }).first().click();
    const reloadedRow = page
      .locator("text=Reaction on your content")
      .locator("xpath=ancestor::div[contains(@class,'flex')][1]");
    const offBtn = reloadedRow.locator("button", { hasText: "Off" });
    await expect(offBtn).toHaveClass(/bg-background/);
  });
});
