import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// Smoke test for the onboarding flow: welcome → workspace → connect AI
// tools. Onboarding asks no questions; the source question is collected
// later by the workspace source-backfill prompt (MUL-5159). Captures
// screenshots for review. Uses a unique email per run so the user is always
// a fresh, un-onboarded user landing on /onboarding.

const EMAIL = `onboarding-v4-${Date.now()}@localhost`;

test.use({ viewport: { width: 1440, height: 900 } });

test("onboarding — welcome → workspace → connect AI tools", async ({ page }, testInfo) => {
  const api = new TestApiClient();
  await api.login(EMAIL, "OBv4 Tester");
  const token = api.getToken();

  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Continue on web");

  // 1. Welcome screen
  await expect(page.getByRole("button", { name: "Continue on web" })).toBeVisible({ timeout: 15000 });
  await page.screenshot({ path: testInfo.outputPath("01-welcome.png"), fullPage: false });

  // 2. Workspace step — straight after Welcome, with no questionnaire.
  await page.getByRole("button", { name: "Continue on web" }).click();
  await expect(page.getByRole("heading", { name: /Name your workspace/i })).toBeVisible({ timeout: 10000 });
  // The rail names every step and marks the current one.
  await expect(page.locator('[data-slot="stepper-title"]')).toHaveText([
    "Workspace",
    "Connect AI tools",
  ]);
  await expect(
    page.locator('[aria-current="step"]').filter({ hasText: "Workspace" }),
  ).toBeVisible();
  await expect(page.getByText("Which best describes you?")).toHaveCount(0);
  await page.waitForTimeout(600);
  await page.screenshot({ path: testInfo.outputPath("02-workspace.png") });

  // 3. Connect AI tools
  await page.getByRole("textbox").first().fill(`Rail QA ${Date.now()}`);
  await page.getByRole("button", { name: /^Create /i }).click();
  await expect(
    page.locator('[aria-current="step"]').filter({ hasText: "Connect AI tools" }),
  ).toBeVisible({ timeout: 20000 });
  await page.waitForTimeout(800);
  await page.screenshot({ path: testInfo.outputPath("03-runtime.png") });
});

test("onboarding — zh-Hans renders Chinese labels", async ({ page, context, baseURL }, testInfo) => {
  await context.addCookies([
    {
      name: "multica-locale",
      value: "zh-Hans",
      url: baseURL ?? "http://localhost:3000",
    },
  ]);
  const api = new TestApiClient();
  await api.login(`zh-${Date.now()}@localhost`, "中文用户");
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem("multica_token", t), token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "在网页端继续");

  // Click the CTA by name. `getByRole("button").first()` used to stand in for
  // it, but the welcome screen renders the pinned Log out button first in DOM
  // order — so this step was signing the user out and the assertions below
  // were waiting on a page that had already redirected to login.
  await page.getByRole("button", { name: "在网页端继续" }).click();

  await expect(page.getByRole("heading", { name: "给工作区起个名字。" })).toBeVisible({ timeout: 10000 });
  await page.waitForTimeout(500);
  await page.screenshot({ path: testInfo.outputPath("02-workspace-zh.png") });
});
