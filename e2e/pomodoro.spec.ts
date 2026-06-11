import { test, expect, type TestInfo } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

function scopeForFocusTest(testInfo: TestInfo): string {
  return `focus-${testInfo.line}`;
}

test.describe("Focus", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    const scope = scopeForFocusTest(testInfo);
    api = await createTestApi(scope);
    await api.clearFocusState();
    await api.clearPomodoroHistory();
    await loginAsDefault(page, scope);
    // Reset any leftover session from a previous test so each test starts clean.
    await api.resetPomodoroSession().catch(() => {});
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  // ── Navigation ──────────────────────────────────────────────────────────────

  test("Focus page is reachable from the sidebar", async ({ page }) => {
    await page.getByRole("link", { name: /focus/i }).click();
    await page.waitForURL("**/focus");
    await expect(page.locator('[aria-label="Current focus"]')).toBeVisible();
  });

  test("legacy Pomodoro route redirects to Focus", async ({ page }) => {
    await page.goto("/pomodoro");
    await page.waitForURL("**/focus");
    await expect(page.locator('[aria-label="Current focus"]')).toBeVisible();
  });

  // ── Empty state ─────────────────────────────────────────────────────────────

  test("Focus page renders the Flowtime-first layout with no session", async ({ page }) => {
    await page.goto("/focus");
    await expect(page.locator('[aria-label="Current focus"]')).toBeVisible({ timeout: 8000 });
    await expect(page.locator('[aria-label="Focus context"]')).toBeVisible();
    await expect(page.getByLabel("Mode")).toHaveValue("flowtime");
    await expect(page.getByLabel("Next step")).toBeVisible();
    await expect(page.getByLabel("Start friction")).toBeVisible();
    await expect(page.getByRole("button", { name: "Start" })).toBeVisible();
  });

  // ── Focus controls ──────────────────────────────────────────────────────────

  test("can start and pause a Flowtime session from the page", async ({ page }) => {
    await page.goto("/focus");

    await page.getByLabel("Next step").fill("Open the failing CI log");
    await page.getByLabel("Note").fill("E2E focus start and pause");

    const startBtn = page.getByRole("button", { name: "Start" });
    await expect(startBtn).toBeVisible({ timeout: 8000 });
    await startBtn.click();

    const pauseBtn = page.getByRole("button", { name: "Pause" });
    await expect(pauseBtn).toBeVisible({ timeout: 5000 });
    await expect(page.getByText("focusing")).toBeVisible();
    await expect(page.getByLabel("Pause reason")).toBeVisible();

    await pauseBtn.click();
    await expect(page.getByRole("button", { name: "Resume" })).toBeVisible({ timeout: 5000 });
    await expect(page.locator('[aria-label="Current focus"]').getByText("paused", { exact: true })).toBeVisible();
  });

  test("can resume complete and enter a suggested break", async ({ page }) => {
    await page.goto("/focus");

    await page.getByLabel("Next step").fill("Finish the focus E2E check");
    await page.getByRole("button", { name: "Start" }).click();
    await expect(page.getByRole("button", { name: "Pause" })).toBeVisible({ timeout: 5000 });

    await page.getByRole("button", { name: "Pause" }).click();
    await expect(page.getByRole("button", { name: "Resume" })).toBeVisible({ timeout: 5000 });

    await page.getByRole("button", { name: "Resume" }).click();
    await expect(page.getByRole("button", { name: "Complete" })).toBeVisible({ timeout: 5000 });

    await page.getByRole("button", { name: "Complete" }).click();
    await expect(page.getByText("Suggested break")).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole("button", { name: "Start break" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Skip" })).toBeVisible();
  });

  test("can start and complete a suggested break", async ({ page }) => {
    await page.goto("/focus");

    await page.getByRole("button", { name: "Start" }).click();
    await expect(page.getByRole("button", { name: "Complete" })).toBeVisible({ timeout: 5000 });
    await page.getByRole("button", { name: "Complete" }).click();

    await expect(page.getByRole("button", { name: "Start break" })).toBeVisible({ timeout: 5000 });
    await page.getByRole("button", { name: "Start break" }).click();

    await expect(page.getByText("Break in progress.")).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole("button", { name: "Complete break" })).toBeVisible();
    await page.getByRole("button", { name: "Complete break" }).click();
    await expect(page.getByRole("button", { name: "Start" })).toBeVisible({ timeout: 5000 });
  });

  test("captures quick-start friction reason before starting", async ({ page }) => {
    await page.goto("/focus");

    await page.getByLabel("Mode").selectOption("quick_start");
    await page.getByLabel("Next step").fill("Write the first sentence");
    await page.getByLabel("Start friction").selectOption("avoidance");
    await page.getByPlaceholder("Optional reason note").fill("E2E avoidance note");

    await page.getByRole("button", { name: "Start" }).click();
    await expect(page.getByText("focusing")).toBeVisible({ timeout: 5000 });
    await expect(page.locator('[aria-label="Current focus"]').getByText("2 min start")).toBeVisible();
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
