import { test, expect, type Page, type TestInfo } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

function scopeForTest(testInfo: TestInfo): string {
  return `time-tracking-${testInfo.line}`;
}

function formatLocalTime(date: Date): string {
  return date.toLocaleTimeString("en-GB", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function getLocalWeekStart(date = new Date()): Date {
  const day = date.getDay();
  const diff = day === 0 ? -6 : 1 - day;
  const monday = new Date(date);
  monday.setDate(date.getDate() + diff);
  monday.setHours(0, 0, 0, 0);
  return monday;
}

async function setDateTimePicker(page: Page, triggerName: string, date: Date) {
  await page.getByRole("button", { name: triggerName }).click();
  const timeInput = page.getByPlaceholder("HH:mm:ss");
  await expect(timeInput).toBeVisible();
  await timeInput.fill(formatLocalTime(date));
  await page.getByRole("button", { name: "Apply" }).click();
}

test.describe("Time tracking", () => {
  let api: TestApiClient | undefined;

  test.beforeEach(async ({ page }, testInfo) => {
    const scope = scopeForTest(testInfo);
    api = await createTestApi(scope);
    await loginAsDefault(page, scope);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
      api = undefined;
    }
  });

  // ── My Time page ───────────────────────────────────────────────────────────

  test("My Time page is reachable", async ({ page }) => {
    await page.goto("/my-time");
    await expect(page.getByRole("button", { name: /add entry/i })).toBeVisible();
  });

  test("Team Time page shows this-week totals grouped by member and project", async ({ page }, testInfo) => {
    const scope = scopeForTest(testInfo);
    const project = await api.createProject({
      title: `E2E Team Time Project ${Date.now()}`,
      icon: "📁",
    });
    const issue = await api.createIssue(`E2E Team Time Issue ${Date.now()}`, {
      project_id: project.id,
    });
    const start = new Date(getLocalWeekStart().getTime() + 60 * 60 * 1000);
    const stop = new Date(start.getTime() + 45 * 60 * 1000);
    await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: stop.toISOString(),
      description: "E2E team time current week",
      issue_id: issue.id,
    });

    await page.goto("/team-time");

    await expect(page.getByRole("heading", { name: "Team Time" })).toBeVisible();
    await expect(page.getByText(/total tracked by the team/i)).toContainText("45:00");
    await expect(page.getByText(`E2E User ${scope}`).first()).toBeVisible();
    await expect(page.getByText(project.title).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "By Member" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "By Project" })).toBeVisible();
  });

  test("Team Time page switches to last month and shows only that range", async ({ page }) => {
    const project = await api.createProject({
      title: `E2E Last Month Project ${Date.now()}`,
      icon: "📁",
    });
    const issue = await api.createIssue(`E2E Last Month Issue ${Date.now()}`, {
      project_id: project.id,
    });
    const start = new Date();
    start.setMonth(start.getMonth() - 1, 15);
    start.setHours(10, 0, 0, 0);
    const stop = new Date(start.getTime() + 30 * 60 * 1000);
    await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: stop.toISOString(),
      description: "E2E team time last month",
      issue_id: issue.id,
    });

    await page.goto("/team-time");

    await expect(page.getByText("No time logged by any member in this period.")).toBeVisible();
    await expect(page.getByText("No project time logged in this period.")).toBeVisible();

    await page.getByRole("button", { name: "Last Month" }).click();

    await expect(page.getByText(project.title).first()).toBeVisible();
    await expect(page.getByText(/total tracked by the team/i)).toContainText("30:00");
    await expect(page.getByText("No time logged by any member in this period.")).not.toBeVisible();
  });

  // ── Start and stop timer via My Time quick entry ───────────────────────────

  test("can start and stop a timer using the My Time quick entry", async ({ page }) => {
    await page.goto("/my-time");

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

    // Quick entry returns to idle state.
    await expect(page.getByPlaceholder(/what are you working on/i)).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole("button", { name: /^start$/i })).toBeVisible();
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
    // Use .first() to avoid strict mode — there may be multiple time displays on the page.
    await expect(
      page.locator("text=10:00").or(page.locator("[class*=badge]").filter({ hasText: /\d+:\d+/ })).first()
    ).toBeVisible({ timeout: 5000 });
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

  // ── Historical overlap warning ─────────────────────────────────────────────

  test("historical overlap shows a warning and requires confirmation", async ({ page }) => {
    const sourceStart = new Date();
    sourceStart.setHours(9, 0, 0, 0);
    const sourceStop = new Date(sourceStart);
    sourceStop.setHours(10, 0, 0, 0);
    const overlapStart = new Date(sourceStart);
    overlapStart.setMinutes(30, 0, 0);
    const overlapStop = new Date(sourceStop);
    overlapStop.setMinutes(30, 0, 0);

    await api.createTimeEntry({
      start_time: sourceStart.toISOString(),
      stop_time: sourceStop.toISOString(),
      description: "Existing overlap source",
    });

    await page.goto("/my-time");
    await page.getByRole("button", { name: /add entry/i }).click();
    await page.getByLabel(/description/i).fill("Potential overlap");
    await setDateTimePicker(page, "Pick start time", overlapStart);
    await setDateTimePicker(page, "Pick stop time", overlapStop);
    await page.getByRole("button", { name: /save entry/i }).click();

    await expect(page.getByText(/may overlap with an existing entry/i)).toBeVisible();
    await page.getByRole("button", { name: /save anyway/i }).click();
    await expect(
      page
        .locator('button[aria-label="Edit time entry"]')
        .filter({ hasText: "Potential overlap" })
        .first()
    ).toBeVisible();
  });

  // ── Delete with confirmation and undo ──────────────────────────────────────

  test("delete requires confirmation and supports undo", async ({ page }) => {
    const now = new Date();
    const start = new Date(now.getTime() - 5 * 60 * 1000);
    const entry = await api.createTimeEntry({
      start_time: start.toISOString(),
      stop_time: now.toISOString(),
      description: "E2E undo delete target",
    });

    await page.goto("/my-time");
    const targetEntryRow = page
      .locator('button[aria-label="Edit time entry"]')
      .filter({ hasText: "E2E undo delete target" })
      .first();
    await expect(targetEntryRow).toBeVisible({ timeout: 5000 });

    // Open edit sheet by clicking the entry.
    await targetEntryRow.click();

    // Click delete button in the edit sheet.
    await page.getByRole("button", { name: /^delete$/i }).click();

    // Confirmation dialog should appear.
    await expect(page.getByRole("dialog", { name: /delete time entry/i })).toBeVisible();
    await page.getByRole("button", { name: /confirm delete/i }).click();

    // Dialog should close and a toast with undo should appear.
    await expect(page.getByRole("dialog", { name: /delete time entry/i })).not.toBeVisible();

    // Look for undo button in toast.
    const undoButton = page.getByRole("button", { name: /undo/i });
    await expect(undoButton).toBeVisible({ timeout: 5000 });

    // Click undo.
    await undoButton.click();

    // Entry should be restored.
    await expect(targetEntryRow).toBeVisible({ timeout: 5000 });

    // Prevent double-cleanup since we restored.
    api["createdTimeEntryIds"] = (api["createdTimeEntryIds"] as string[]).filter(
      (id) => id !== entry.id,
    );
  });

  // ── Switch confirmation ────────────────────────────────────────────────────

  test("switching from another issue asks for confirmation before replacing the current timer", async ({ page }) => {
    const issue1 = await api.createIssue("Switch source");
    const issue2 = await api.createIssue("Switch target");
    await api.startTimer({ issue_id: issue1.id, description: "Original timer" });

    await page.goto(`/issues/${issue2.id}`);
    await page.getByRole("button", { name: /switch timer/i }).click();
    await page.getByRole("textbox", { name: /what are you working on\?/i }).fill("Replacement timer");
    await page.getByRole("button", { name: /switch & start/i }).click();
    await expect(page.getByRole("dialog", { name: /switch timer/i })).toBeVisible();
    await page.getByRole("button", { name: /confirm switch/i }).click();
    await expect(page.getByRole("button", { name: /^stop$/i })).toBeVisible();
  });

  // ── Historical entry creation ──────────────────────────────────────────────

  test("can create a historical time entry without an issue link", async ({ page }) => {
    const start = new Date();
    start.setHours(9, 0, 0, 0);
    const stop = new Date(start);
    stop.setMinutes(45, 0, 0);

    await page.goto("/my-time");
    await page.getByRole("button", { name: /add entry/i }).click();
    await page.getByLabel(/description/i).fill("Historical admin work");
    await setDateTimePicker(page, "Pick start time", start);
    await setDateTimePicker(page, "Pick stop time", stop);
    await page.getByRole("button", { name: /save entry/i }).click();
    await expect(
      page
        .locator('button[aria-label="Edit time entry"]')
        .filter({ hasText: "Historical admin work" })
        .first()
    ).toBeVisible();
  });
});
