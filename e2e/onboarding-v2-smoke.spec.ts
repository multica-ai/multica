import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// Smoke test for Onboarding V2: verifies the new per-question flow
// renders and captures screenshots for review. Uses a unique email
// per run so the user is always a fresh, un-onboarded user landing
// on /onboarding.

const EMAIL = `onboarding-v2-${Date.now()}@localhost`;
const SHOTS_DIR = "/tmp/onboarding-v2-shots";

test.use({ viewport: { width: 1440, height: 900 } });

test("onboarding v2 — welcome → source → role → use_case (skip path)", async ({ page }) => {
  const api = new TestApiClient();
  await api.login(EMAIL, "OBv2 Tester");
  const token = api.getToken();

  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Continue on web");

  // 1. Welcome screen
  await expect(page.getByRole("button", { name: "Continue on web" })).toBeVisible({ timeout: 15000 });
  await page.screenshot({ path: `${SHOTS_DIR}/01-welcome.png`, fullPage: false });

  // Click Start exploring to advance to Source
  await page.getByRole("button", { name: "Continue on web" }).click();

  // 2. Source step
  await expect(page.getByText("How did you hear about Multica?")).toBeVisible({ timeout: 10000 });
  await expect(page.getByText(/Step 1 of \d+/)).toBeVisible();
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/02-source.png` });

  // Pick Friends/colleagues then click Continue to advance.
  await page.getByRole("radio", { name: /Friends or colleagues/i }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  // 3. Role step
  await expect(page.getByText("Which best describes you?")).toBeVisible({ timeout: 10000 });
  await expect(page.getByText(/Step 2 of \d+/)).toBeVisible();
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/03-role.png` });

  // Skip role
  await page.getByRole("button", { name: "Skip" }).click();

  // 4. Use case step
  await expect(page.getByText("What do you want to use Multica for?")).toBeVisible({ timeout: 10000 });
  await expect(page.getByText(/Step 3 of \d+/)).toBeVisible();
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/04-use-case.png` });

  // Pick ship_code then Continue → workspace step.
  await page.getByRole("checkbox", { name: /Ship code with AI agents/i }).click();
  await page.getByRole("button", { name: "Continue" }).click();

  // 5. Workspace step (legacy)
  await expect(page.getByRole("heading", { name: /Name your workspace/i })).toBeVisible({ timeout: 10000 });
  await page.screenshot({ path: `${SHOTS_DIR}/05-workspace.png` });
});

test("onboarding v2 — rage-skip all 3 questions", async ({ page }) => {
  const api = new TestApiClient();
  await api.login(`rage-skip-${Date.now()}@localhost`, "Rage Skipper");
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem("multica_token", t), token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Continue on web");

  await page.getByRole("button", { name: "Continue on web" }).click();
  await expect(page.getByText("How did you hear about Multica?")).toBeVisible({ timeout: 10000 });

  // Skip × 3
  await page.getByRole("button", { name: "Skip" }).click();
  await expect(page.getByText("Which best describes you?")).toBeVisible({ timeout: 10000 });
  await page.getByRole("button", { name: "Skip" }).click();
  await expect(page.getByText("What do you want to use Multica for?")).toBeVisible({ timeout: 10000 });
  await page.getByRole("button", { name: "Skip" }).click();

  // Lands on workspace step
  await expect(page.getByRole("heading", { name: /Name your workspace/i })).toBeVisible({ timeout: 10000 });
  await page.screenshot({ path: `${SHOTS_DIR}/06-after-rage-skip.png` });
});

test("onboarding v2 — zh-Hans renders Chinese labels", async ({ page, context }) => {
  await context.addCookies([
    { name: "multica-locale", value: "zh-Hans", url: "http://localhost:13442" },
  ]);
  const api = new TestApiClient();
  await api.login(`zh-${Date.now()}@localhost`, "中文用户");
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem("multica_token", t), token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "在 web 端继续");

  await page.getByRole("button").first().click().catch(() => {});

  // Source screen — Chinese question
  await expect(page.getByText("你是从哪里了解到 Multica 的？")).toBeVisible({ timeout: 10000 });
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/07-source-zh.png` });
});
