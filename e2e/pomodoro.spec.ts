import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Pomodoro", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi("pomodoro");
    await loginAsDefault(page, "pomodoro");
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

  test("Pomodoro history page shows empty state when there are no sessions", async ({ page }) => {
    await page.goto("/pomodoro");
    await expect(page.getByText(/no pomodoro sessions yet/i)).toBeVisible({ timeout: 8000 });
    await expect(page.getByText(/start your first one/i)).toBeVisible();
  });

  // ── Timer widget ────────────────────────────────────────────────────────────

  test("Pomodoro timer widget shows work phase by default", async ({ page }) => {
    await page.goto("/issues");
    // The timer widget is rendered in the sidebar. "专注" is the work-phase label.
    await expect(page.getByText("专注")).toBeVisible({ timeout: 8000 });
  });

  test("can start and pause the pomodoro timer via the sidebar widget", async ({ page }) => {
    await page.goto("/issues");

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

    // The reset button should be visible.
    const resetBtn = page.getByRole("button", { name: "重置番茄钟" });
    await expect(resetBtn).toBeVisible({ timeout: 8000 });

    await resetBtn.click();

    // After reset the start button should reappear (idle state).
    await expect(page.getByRole("button", { name: "开始番茄钟" })).toBeVisible({ timeout: 5000 });
  });

  test("document title updates while the timer is running", async ({ page }) => {
    await page.goto("/issues");

    const startBtn = page.getByRole("button", { name: "开始番茄钟" });
    await expect(startBtn).toBeVisible({ timeout: 8000 });
    await startBtn.click();

    // Title should contain the 🍅 emoji while running.
    await expect(page).toHaveTitle(/🍅/, { timeout: 5000 });

    // Pause to clean up.
    await page.getByRole("button", { name: "暂停番茄钟" }).click();
  });

  // ── History & stats ─────────────────────────────────────────────────────────

  test("completed pomodoro appears in the history page", async ({ page }) => {
    // Create a pomodoro entry: start → complete (backend writes type=pomodoro time_entry).
    await api.startPomodoroSession();
    await api.completePomodoroSession({ note: "E2E pomodoro test" });

    await page.goto("/pomodoro");

    // The history section should now render — entry is shown with the 🍅 emoji row.
    await expect(page.getByText("History")).toBeVisible({ timeout: 8000 });
    await expect(page.getByText("E2E pomodoro test")).toBeVisible({ timeout: 5000 });
  });

  test("today stats card increments after completing a pomodoro", async ({ page }) => {
    // Complete a fresh pomodoro session via the API.
    await api.startPomodoroSession();
    await api.completePomodoroSession();

    await page.goto("/pomodoro");

    // The "Today" stat card should show at least 1.
    await expect(
      page.locator(".rounded-lg.border", { hasText: "Today" }).getByText(/^[1-9]\d*$/)
    ).toBeVisible({ timeout: 8000 });
  });

  test("Pomodoro history shows Today and This Week stat cards", async ({ page }) => {
    await page.goto("/pomodoro");

    // Stat cards are always rendered (showing 0 when no data).
    await expect(page.getByText("Today")).toBeVisible({ timeout: 8000 });
    await expect(page.getByText("This Week")).toBeVisible();
    await expect(page.getByText("Total Focus")).toBeVisible();
    await expect(page.getByText(/\dd$/)).toBeVisible(); // Streak card (e.g. "0d")
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
