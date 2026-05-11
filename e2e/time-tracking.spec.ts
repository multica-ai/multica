import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Time tracking", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi("time-tracking");
    await loginAsDefault(page, "time-tracking");
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  // ── My Time page ───────────────────────────────────────────────────────────

  test("My Time page is reachable from the sidebar", async ({ page }) => {
    await page.getByRole("link", { name: /my time/i }).click();
    await page.waitForURL("**/my-time");
    await expect(page.getByRole("heading", { name: /my time/i })).toBeVisible();
  });

  // ── Start and stop timer via sidebar widget ─────────────────────────────────

  test("can start and stop a timer using the sidebar widget", async ({ page }) => {
    await page.goto("/issues");

    // Open the inline description form.
    await page.getByRole("button", { name: /track time/i }).click();

    // The description input should appear.
    const descInput = page.getByPlaceholder(/what are you working on/i);
    await expect(descInput).toBeVisible();
    await descInput.fill("E2E sidebar timer");

    // Start the timer.
    await page.getByRole("button", { name: /^start$/i }).click();

    // The Stop button (square icon) should appear in the widget.
    await expect(page.getByRole("button", { name: /stop timer/i })).toBeVisible({ timeout: 5000 });

    // Stop it.
    await page.getByRole("button", { name: /stop timer/i }).click();

    // Widget returns to idle state.
    await expect(page.getByRole("button", { name: /track time/i })).toBeVisible({ timeout: 5000 });
  });

  // ── Manual entry on My Time page ───────────────────────────────────────────

  test("manual time entry appears on My Time page", async ({ page }) => {
    const now = new Date();
    const start = new Date(now.getTime() - 30 * 60 * 1000); // 30 min ago
    const entry = await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: now.toISOString(),
      description: "E2E manual entry",
    });

    await page.goto("/my-time");

    await expect(page.getByText("E2E manual entry")).toBeVisible({ timeout: 5000 });

    // Clean up.
    await api.deleteTimeEntry(entry.id as string);
  });

  // ── Issue detail time tracking section ────────────────────────────────────

  test("issue detail shows Time section with Start button", async ({ page }) => {
    const issue = await api.createIssue("E2E Timer Issue");

    await page.goto(`/issues/${issue.id}`);

    // The time section header should be visible.
    await expect(page.getByRole("heading", { name: /^time$/i })).toBeVisible({ timeout: 5000 });

    // The Start button should be present.
    await expect(page.getByRole("button", { name: /^start$/i })).toBeVisible();
  });

  test("can start and stop a timer from an issue detail panel", async ({ page }) => {
    const issue = await api.createIssue("E2E Issue Timer");

    await page.goto(`/issues/${issue.id}`);

    // Open the inline description form.
    await page.getByRole("button", { name: /^start$/i }).click();

    const descInput = page.getByPlaceholder(/what are you working on/i);
    await expect(descInput).toBeVisible();
    await descInput.fill("Working on this issue");
    await page.getByRole("button", { name: /^start$/i }).last().click();

    // The Stop button should now be visible.
    await expect(page.getByRole("button", { name: /^stop$/i })).toBeVisible({ timeout: 5000 });

    // Stop the timer.
    await page.getByRole("button", { name: /^stop$/i }).click();

    // Start button should return.
    await expect(page.getByRole("button", { name: /^start$/i })).toBeVisible({ timeout: 5000 });
  });

  test("timer linked to issue appears in issue entry list", async ({ page }) => {
    const issue = await api.createIssue("E2E Linked Timer Issue");

    const now = new Date();
    const start = new Date(now.getTime() - 10 * 60 * 1000); // 10 min ago
    await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: now.toISOString(),
      description: "E2E linked entry",
      issue_id: issue.id,
    });

    await page.goto(`/issues/${issue.id}`);

    // The entry should be visible in the time section.
    await expect(page.getByText("E2E linked entry")).toBeVisible({ timeout: 5000 });

    // Total badge should be visible (non-zero).
    await expect(page.locator("text=10:00").or(page.locator("[class*=badge]").filter({ hasText: /\d+:\d+/ }))).toBeVisible({ timeout: 5000 });
  });

  // ── Switch timer flow ──────────────────────────────────────────────────────

  test("Switch timer button appears when another timer is running", async ({ page }) => {
    const issue1 = await api.createIssue("E2E Issue 1");
    const issue2 = await api.createIssue("E2E Issue 2");

    // Start a timer on issue1 via API.
    const running = await api.startTimer({ issue_id: issue1.id, description: "Issue 1 timer" });

    // Navigate to issue2 — should see "Switch timer" button instead of "Start".
    await page.goto(`/issues/${issue2.id}`);

    await expect(page.getByRole("button", { name: /switch timer/i })).toBeVisible({ timeout: 5000 });

    // Stop via API to clean up.
    await api.stopTimer(running.id as string);
  });

  // ── Delete entry ───────────────────────────────────────────────────────────

  test("can delete a time entry from the My Time page", async ({ page }) => {
    const now = new Date();
    const start = new Date(now.getTime() - 5 * 60 * 1000);
    const entry = await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: now.toISOString(),
      description: "E2E delete me",
    });

    await page.goto("/my-time");

    await expect(page.getByText("E2E delete me")).toBeVisible({ timeout: 5000 });

    // Open the entry options menu.
    const row = page.locator("[data-testid='time-entry-row']", { hasText: "E2E delete me" })
      .or(page.locator(".group", { hasText: "E2E delete me" }));
    await row.hover();
    await row.getByRole("button", { name: /options/i }).click();

    // Click Delete.
    await page.getByRole("menuitem", { name: /delete/i }).click();

    // Entry should be gone.
    await expect(page.getByText("E2E delete me")).not.toBeVisible({ timeout: 5000 });

    // Prevent double-cleanup.
    api["createdTimeEntryIds"] = (api["createdTimeEntryIds"] as string[]).filter(
      (id) => id !== entry.id,
    );
  });
});
