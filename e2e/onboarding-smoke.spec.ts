import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// Smoke test for web onboarding: welcome → Start (the workspace is created
// for the member, no naming step) → connect a computer → skip into the
// workspace. Uses a unique email per run so the user is always a fresh,
// un-onboarded user landing on /onboarding.

const SHOTS_DIR = "../shots-onboarding";

test.use({ viewport: { width: 1440, height: 900 } });

async function startOnboarding(
  page: import("@playwright/test").Page,
  email: string,
  name: string,
) {
  const api = new TestApiClient();
  await api.login(email, name);
  await page.addInitScript(
    (t) => localStorage.setItem("multica_token", t),
    api.getToken(),
  );
  return api;
}

test("onboarding — Start creates the workspace and opens the connect step", async ({ page }) => {
  const api = await startOnboarding(page, `onboarding-${Date.now()}@localhost`, "Ada Tester");
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Your AI teammates, in one workspace.");

  // Web has one more step than desktop: connecting a computer.
  await expect(page.getByText("Connect a computer")).toBeVisible();
  await expect(page.getByText("Pick a coding tool")).toBeVisible();
  await expect(page.getByText("Give it a first task")).toBeVisible();
  // Nothing on web can see this computer's tools.
  await expect(page.getByText(/on this computer/)).toHaveCount(0);
  await page.screenshot({ path: `${SHOTS_DIR}/01-welcome.png` });

  await page.getByRole("button", { name: "Start" }).click();

  await expect(page.getByRole("heading", { name: "Connect a computer" })).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByRole("progressbar", { name: "Step 1 of 3" })).toBeVisible();
  const workspaces = await api.getWorkspaces();
  expect(workspaces.map((w) => w.name)).toEqual(["Ada's workspace"]);
  await page.screenshot({ path: `${SHOTS_DIR}/02-connect.png` });

  await page.getByRole("button", { name: "Skip for now" }).click();
  await page.waitForURL(/\/issues/, { timeout: 20000 });
});

test("onboarding — back from the first step returns to welcome without a second workspace", async ({
  page,
}) => {
  const api = await startOnboarding(page, `onboarding-back-${Date.now()}@localhost`, "Back Tester");
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Your AI teammates, in one workspace.");

  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("heading", { name: "Connect a computer" }).waitFor({ timeout: 20000 });
  await page.getByRole("button", { name: "Back" }).click();
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("heading", { name: "Connect a computer" }).waitFor({ timeout: 20000 });

  expect(await api.getWorkspaces()).toHaveLength(1);
});

test("onboarding — zh-Hans renders Chinese labels", async ({ page, context, baseURL }) => {
  await context.addCookies([
    {
      name: "multica-locale",
      value: "zh-Hans",
      url: baseURL ?? "http://localhost:3000",
    },
  ]);
  await startOnboarding(page, `zh-${Date.now()}@localhost`, "中文用户");
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "你的 AI 队友，在同一个工作区。");

  // Click the CTA by name: the header's Log out button comes first in DOM
  // order, so `getByRole("button").first()` would sign the user out.
  await page.getByRole("button", { name: "开始" }).click();

  await expect(page.getByRole("heading", { name: "连接一台电脑" })).toBeVisible({
    timeout: 20000,
  });
  await page.screenshot({ path: `${SHOTS_DIR}/03-connect-zh.png` });
});
