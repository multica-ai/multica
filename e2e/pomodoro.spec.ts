import { test, expect, type TestInfo } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

function scopeForPomodoroTest(testInfo: TestInfo): string {
  return `pomodoro-${testInfo.line}`;
}

test.describe("Pomodoro", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    const scope = scopeForPomodoroTest(testInfo);
    api = await createTestApi(scope);
    await api.clearPomodoroHistory();
    await loginAsDefault(page, scope);
    // Reset any leftover session from a previous test so each test starts clean.
    await api.resetPomodoroSession().catch(() => {});
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  // ── Navigation ──────────────────────────────────────────────────────────────

  test("Pomodoro page is reachable from the sidebar", async ({ page }) => {
    await page.getByRole("link", { name: /pomodoro/i }).click();
    await page.waitForURL("**/pomodoro");
    await expect(page.getByRole("heading", { name: /pomodoro/i })).toBeVisible();
  });

  // ── Empty state ─────────────────────────────────────────────────────────────

  test("Pomodoro page renders the focus-first layout even with no sessions", async ({ page }) => {
    await page.goto("/pomodoro");
    // Focus-first shell is always present regardless of session history.
    await expect(page.getByRole("heading", { name: "Pomodoro" })).toBeVisible({ timeout: 8000 });
    await expect(page.getByText("Focus mode first. History stays below.")).toBeVisible();
    await expect(page.getByText("Recent sessions")).toBeVisible();
  });

  // ── Timer widget ────────────────────────────────────────────────────────────

  test("Pomodoro timer widget shows work phase by default", async ({ page }) => {
    await page.goto("/issues");
    // Switch to pomodoro mode — the sidebar widget defaults to normal timer mode.
    await page.getByText("番茄钟").click();
    // The timer widget is rendered in the sidebar. "专注" is the work-phase label.
    await expect(page.getByText("专注")).toBeVisible({ timeout: 8000 });
  });

  test("can start and pause the pomodoro timer via the sidebar widget", async ({ page }) => {
    await page.goto("/issues");

    // Switch to pomodoro mode — the sidebar widget defaults to normal timer mode.
    await page.getByText("番茄钟").click();

    // Wait for the widget to finish loading the session from the server.
    const startBtn = page.getByRole("button", { name: "开始番茄钟" });
    await expect(startBtn).toBeVisible({ timeout: 8000 });

    // Start the timer.
    await startBtn.click();

    // The pause button should now be present.
    const pauseBtn = page.getByRole("button", { name: "暂停番茄钟" });
    await expect(pauseBtn).toBeVisible({ timeout: 5000 });

    // Pause it.
    await pauseBtn.click();

    // After pausing the start button returns.
    await expect(page.getByRole("button", { name: "开始番茄钟" })).toBeVisible({ timeout: 5000 });
  });

  test("can reset the pomodoro timer via the sidebar widget", async ({ page }) => {
    // Start a session via the API so the timer is in a paused state.
    await api.startPomodoroSession();
    await api.pausePomodoroSession();

    await page.goto("/issues");

    // Switch to pomodoro mode — the sidebar widget defaults to normal timer mode.
    await page.getByText("番茄钟").click();

    // The reset button should be visible.
    const resetBtn = page.getByRole("button", { name: "重置番茄钟" });
    await expect(resetBtn).toBeVisible({ timeout: 8000 });

    await resetBtn.click();

    // After reset the start button should reappear (idle state).
    await expect(page.getByRole("button", { name: "开始番茄钟" })).toBeVisible({ timeout: 5000 });
  });

  test("document title updates while the timer is running", async ({ page }) => {
    await page.goto("/issues");

    // Switch to pomodoro mode — the sidebar widget defaults to normal timer mode.
    await page.getByText("番茄钟").click();

    const startBtn = page.getByRole("button", { name: "开始番茄钟" });
    await expect(startBtn).toBeVisible({ timeout: 8000 });
    await startBtn.click();

    // Title should contain the 🍅 emoji while running.
    await expect(page).toHaveTitle(/🍅/, { timeout: 5000 });

    // Pause to clean up.
    await page.getByRole("button", { name: "暂停番茄钟" }).click();
  });

  // ── Focus-first layout ──────────────────────────────────────────────────────

  test("pomodoro page shows the focus-first hero before history", async ({ page }) => {
    await api.startPomodoroSession();
    await page.goto("/pomodoro");

    await expect(page.getByRole("heading", { name: "Pomodoro" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Current session" })).toBeVisible();
    await expect(page.getByText("Quick capture")).toBeVisible();
    await expect(page.getByText("Recent sessions")).toBeVisible();
    await expect(page.getByRole("button", { name: "Expand full history" })).toBeVisible();
  });

  // ── History & stats ─────────────────────────────────────────────────────────

  test("completed pomodoro appears in the recent sessions list", async ({ page }) => {
    // Create a pomodoro entry: start → complete (backend writes type=pomodoro time_entry).
    await api.startPomodoroSession();
    await api.completePomodoroSession({ note: "E2E pomodoro test" });

    await page.goto("/pomodoro");

    // The recent sessions section should now render — entry is shown in the list.
    await expect(page.getByText("Recent sessions")).toBeVisible({ timeout: 8000 });
    await expect(page.getByText("E2E pomodoro test")).toBeVisible({ timeout: 5000 });
  });

  test("today summary increments after completing a pomodoro", async ({ page }) => {
    // Complete a fresh pomodoro session via the API so there is exactly 1 done entry for today.
    await api.startPomodoroSession();
    await api.completePomodoroSession();

    await page.goto("/pomodoro");

    // The Today summary card must be visible and its "Done today" counter must read "1".
    const todaySection = page.locator('[aria-label="Today"]');
    await expect(todaySection).toBeVisible({ timeout: 8000 });
    // Locate the stat cell that is labelled "Done today" and verify the count value.
    const doneTodayCell = todaySection.locator("div").filter({ hasText: /^Done today/ }).first();
    await expect(doneTodayCell.locator("p").last()).toHaveText("1", { timeout: 8000 });
  });

  test("today summary shows capped progress after hitting the focus target", async ({ page }) => {
    const now = new Date();
    for (let index = 0; index < 6; index += 1) {
      const start = new Date(now.getTime() - (index + 1) * 40 * 60 * 1000);
      const stop = new Date(start.getTime() + 25 * 60 * 1000);
      await api.createPomodoroHistoryEntry({
        start_time: start.toISOString(),
        stop_time: stop.toISOString(),
        description: `E2E pomodoro target ${index + 1}`,
      });
    }

    await page.goto("/pomodoro");

    const todaySection = page.locator('[aria-label="Today"]');
    await expect(todaySection).toBeVisible({ timeout: 8000 });
    await expect(todaySection.getByText("Focus target")).toBeVisible();
    await expect(todaySection.getByText("6 pomodoros")).toBeVisible();
    await expect(todaySection.getByText("Done today")).toBeVisible();
    await expect(todaySection.getByText("100%")).toBeVisible();
    const remainingCell = todaySection.locator("div").filter({ hasText: /^Remaining/ }).first();
    await expect(remainingCell.locator("p").last()).toHaveText("0");
  });

  test("today summary shows a two-day streak from consecutive local days", async ({ page }) => {
    const todayStart = new Date();
    todayStart.setHours(10, 0, 0, 0);
    const yesterdayStart = new Date(todayStart);
    yesterdayStart.setDate(yesterdayStart.getDate() - 1);
    const createSession = async (start: Date, label: string) => {
      await api.createPomodoroHistoryEntry({
        start_time: start.toISOString(),
        stop_time: new Date(start.getTime() + 25 * 60 * 1000).toISOString(),
        description: label,
      });
    };

    await createSession(todayStart, "E2E streak today");
    await createSession(yesterdayStart, "E2E streak yesterday");

    await page.goto("/pomodoro");

    const todaySection = page.locator('[aria-label="Today"]');
    await expect(todaySection).toBeVisible({ timeout: 8000 });
    const streakCell = todaySection.locator("div").filter({ hasText: /^Streak/ }).first();
    await expect(streakCell.locator("p").last()).toHaveText("2 days");
  });

  // ── Settings ────────────────────────────────────────────────────────────────

  test("Settings page includes a Pomodoro tab with time interval inputs", async ({ page }) => {
    await page.goto("/settings");

    // Click the Pomodoro tab.
    await page.getByRole("tab", { name: /pomodoro/i }).click();

    // The Work Duration input should be visible.
    await expect(page.getByLabel(/work duration/i)).toBeVisible({ timeout: 5000 });

    // Short Break and Long Break inputs should also be present.
    await expect(page.getByLabel(/short break/i)).toBeVisible();
    await expect(page.getByLabel(/long break$/i)).toBeVisible();
  });

  test("Pomodoro settings update persists work duration change", async ({ page }) => {
    await page.goto("/settings");
    await page.getByRole("tab", { name: /pomodoro/i }).click();

    const input = page.getByLabel(/work duration/i);
    await expect(input).toBeVisible({ timeout: 5000 });

    // Clear the field and type a new value.
    await input.fill("30");

    // Navigate away and back — value should be persisted in localStorage.
    await page.goto("/issues");
    await page.goto("/settings");
    await page.getByRole("tab", { name: /pomodoro/i }).click();

    await expect(page.getByLabel(/work duration/i)).toHaveValue("30");

    // Restore default so we don't pollute other tests.
    await page.getByLabel(/work duration/i).fill("25");
  });

  test("Pomodoro settings page has Sound section with white noise selector", async ({ page }) => {
    await page.goto("/settings");
    await page.getByRole("tab", { name: /pomodoro/i }).click();

    // Sound section.
    await expect(page.getByText(/sound effects/i)).toBeVisible({ timeout: 5000 });
    // White noise select trigger.
    await expect(page.getByText(/white noise/i)).toBeVisible();
  });
});
