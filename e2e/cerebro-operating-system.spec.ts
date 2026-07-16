import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

test.describe("Cerebro operating system", () => {
  let api: TestApiClient;
  let slug: string;
  let strategyTitle: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_operating_system", true);
    await request(api, "/api/cerebro/operating-system/settings", {
      method: "PUT",
      body: JSON.stringify({ terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" } }),
    });
    slug = await loginAsDefault(page);
    strategyTitle = `E2E horizon ${Date.now()}`;
  });

  test.afterEach(async () => {
    if (!api) return;
    const response = await request(api, "/api/cerebro/strategy-items");
    if (response.ok) {
      const data = await response.json() as { strategy_items?: Array<{ id: string; title: string }> };
      for (const item of data.strategy_items ?? []) {
        if (item.title.startsWith("E2E v4 Strategy") || item.title.startsWith("E2E horizon")) await request(api, `/api/cerebro/strategy-items/${item.id}`, { method: "DELETE" });
      }
    }
    const rocksResponse = await request(api, "/api/cerebro/rocks");
    if (rocksResponse.ok) {
      const data = await rocksResponse.json() as { rocks?: Array<{ id: string; title: string }> };
      for (const rock of data.rocks ?? []) {
        if (rock.title.startsWith("E2E v4 Rock")) await request(api, `/api/cerebro/rocks/${rock.id}`, { method: "DELETE" });
      }
    }
    await api.cleanup();
  });

  test("creates and edits a first-class Rock with Projects, Issues, Strategy, check-ins, rollup, and history", async ({ page }) => {
    const requests: string[] = [];
    const serverErrors: string[] = [];
    page.on("request", (request) => requests.push(request.url()));
    page.on("response", (response) => {
      if (response.status() >= 500) serverErrors.push(`${response.status()} ${response.url()}`);
    });
    const suffix = Date.now();
    const projectOne = await api.createProject(`E2E v4 Project A ${suffix}`);
    const projectTwo = await api.createProject(`E2E v4 Project B ${suffix}`);
    const standaloneIssue = await api.createIssue(`E2E v4 standalone issue ${suffix}`);
    const blockedIssue = await api.createIssue(`E2E v4 blocked issue ${suffix}`, { project_id: projectOne.id, status: "blocked" });
    await api.createIssue(`E2E v4 done issue ${suffix}`, { project_id: projectOne.id, status: "done" });
    await api.createIssue(`E2E v4 second project issue ${suffix}`, { project_id: projectTwo.id, status: "in_progress" });
    await request(api, "/api/cerebro/strategy-items", { method: "POST", body: JSON.stringify({ kind: "core_value", title: `E2E v4 Strategy value ${suffix}`, description: "Own the outcome", position: 0, state: "active" }) });
    await request(api, "/api/cerebro/strategy-items", { method: "POST", body: JSON.stringify({ kind: "core_focus", title: `E2E v4 Strategy focus ${suffix}`, description: "Commerce operations", position: 1, state: "active" }) });

    await page.goto(`/${slug}/strategy`);
    await page.getByRole("button", { name: "Edit map" }).click();
    await page.getByRole("button", { name: "+ Add item" }).click();
    await page.getByLabel("Type").selectOption("horizon_goal");
    await page.getByLabel("Title").fill(strategyTitle);
    await page.getByLabel("Horizon name").fill("FY27 North Star");
    await page.getByLabel("Horizon count").fill("18");
    await page.getByLabel("Horizon unit").selectOption("month");
    await page.getByRole("button", { name: "Save Strategy item" }).click();
    await expect(page.getByText(strategyTitle)).toBeVisible();

    await page.goto(`/${slug}/rocks`);
    await page.getByRole("button", { name: "+ New Rock" }).click();
    await page.getByLabel("Rock title").fill(`E2E v4 Rock ${suffix}`);
    await page.getByLabel("Description").fill("A standalone outcome with optional execution links.");
    await page.getByLabel("Owner").selectOption({ label: "E2E User" });
    await page.getByLabel("Confidence").fill("80");
    await page.getByLabel("Reported health").selectOption("on_track");
    await page.getByLabel("Strategy connection").selectOption({ label: strategyTitle });
    await page.getByLabel("Projects").selectOption([projectOne.id, projectTwo.id]);
    await page.getByLabel("Issues").selectOption([standaloneIssue.id]);
    await page.getByRole("button", { name: "Save Rock" }).click();
    const originalTitle = `E2E v4 Rock ${suffix}`;
    await expect(page.getByText(originalTitle)).toBeVisible();
    await expect(page.getByText("1/4 issues · 2 projects")).toBeVisible();

    await page.getByRole("button", { name: `View ${originalTitle}` }).click();
    await expect(page.getByText(blockedIssue.title)).toBeVisible();
    await expect(page.getByText(standaloneIssue.title)).toBeVisible();
    await page.getByLabel("Check-in confidence").fill("42");
    await page.getByLabel("Check-in health").selectOption("at_risk");
    await page.getByLabel("Check-in note").fill("Customer dependency needs attention.");
    await page.getByRole("button", { name: "Save check-in" }).click();
    await expect(page.getByText("Customer dependency needs attention.")).toBeVisible();

    await page.getByRole("button", { name: "Edit Rock" }).click();
    const editedTitle = `E2E v4 Rock edited ${suffix}`;
    await page.getByLabel("Rock title").fill(editedTitle);
    await page.getByLabel("Confidence", { exact: true }).fill("65");
    await page.getByLabel("Reported health").selectOption("at_risk");
    await page.getByRole("button", { name: "Save Rock" }).click();
    await expect(page.getByRole("button", { name: `View ${editedTitle}` })).toBeVisible();

    await page.goto(`/${slug}/strategy`);
    await expect(page.getByRole("heading", { name: "FY27 North Star" })).toBeVisible();
    await expect(page.getByText(editedTitle)).toBeVisible();
    await expect(page.getByText("1/4 issues · 65% confidence")).toBeVisible();
    await expect(page.getByText(`E2E v4 Strategy value ${suffix}`)).toBeVisible();
    await expect(page.getByText(`E2E v4 Strategy focus ${suffix}`)).toBeVisible();

    await page.getByRole("button", { name: "Edit map" }).click();
    await page.getByRole("heading", { name: strategyTitle }).locator("xpath=ancestor::article").getByRole("button", { name: "Edit" }).click();
    const editedStrategyTitle = `E2E horizon edited ${suffix}`;
    await page.getByLabel("Title").fill(editedStrategyTitle);
    await page.getByRole("button", { name: "Save Strategy item" }).click();
    await expect(page.getByText(editedStrategyTitle).first()).toBeVisible();

    const strategyResponse = await request(api, "/api/cerebro/strategy-items");
    const strategyData = await strategyResponse.json() as { strategy_items: Array<{ id: string; title: string }> };
    const editedStrategy = strategyData.strategy_items.find((item) => item.title === editedStrategyTitle);
    expect(editedStrategy).toBeTruthy();
    await request(api, `/api/cerebro/strategy-items/${editedStrategy!.id}`, { method: "DELETE" });
    await page.reload();
    await page.getByRole("button", { name: "History" }).click();
    await expect(page.getByRole("heading", { name: "Strategy history" })).toBeVisible();
    await expect(page.getByText("updated", { exact: true }).first()).toBeVisible();
    await expect(page.getByText("deleted", { exact: true }).first()).toBeVisible();
    await expect(page.getByText(editedStrategyTitle).first()).toBeVisible();

    expect(requests.some((url) => url.toLowerCase().includes("atlas"))).toBe(false);
    expect(serverErrors).toEqual([]);
  });

  test(
    "keeps Rocks and Strategy usable at desktop and phone widths",
    async ({ page }, testInfo) => {
      for (const viewport of [
        { name: "desktop", width: 1440, height: 900 },
        { name: "phone", width: 390, height: 844 },
      ]) {
        await page.setViewportSize(viewport);

        for (const route of ["rocks", "strategy"] as const) {
          await page.goto(`/${slug}/${route}`);
          await expect(page.getByRole("main").last()).toBeVisible();
          await expect
            .poll(async () =>
              page.evaluate(
                () => document.documentElement.scrollWidth <= window.innerWidth,
              ),
            )
            .toBe(true);
          await expect(
            page.getByRole("button", {
              name: route === "rocks" ? "+ New Rock" : "Edit map",
            }),
          ).toBeVisible();
          await page.screenshot({
            path: testInfo.outputPath(`${route}-${viewport.name}.png`),
            fullPage: true,
          });
        }
      }

      await page.setViewportSize({ width: 1440, height: 900 });
      await page.goto(`/${slug}/rocks`);
      const newRockButton = page.getByRole("button", { name: "+ New Rock" });
      await newRockButton.focus();
      await expect(newRockButton).toBeFocused();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("heading", { name: "New Rock" })).toBeVisible();
      const cancelButton = page.getByRole("button", { name: "Cancel" });
      await cancelButton.focus();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("heading", { name: "New Rock" })).toHaveCount(0);
    },
  );

  test("keeps terminology in Settings and gates routes and navigation with the feature flag", async ({ page }) => {
    await page.goto(`/${slug}/settings`);
    await page.getByRole("tab", { name: "Operating System" }).click();
    await page.getByRole("combobox", { name: "Profile" }).selectOption("custom");
    await page.getByLabel("Strategy label").fill("Direction");
    await page.getByLabel("Rock label").fill("Priority");
    await page.getByLabel("Rocks label").fill("Priorities");
    await page.getByRole("button", { name: "Save terminology" }).click();

    await page.goto(`/${slug}/rocks`);
    await expect(page.getByRole("heading", { name: "Priorities", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Priorities" })).toBeVisible();
    await expect(page.getByLabel("Strategy label")).toHaveCount(0);
    await page.goto(`/${slug}/strategy`);
    await expect(page.getByRole("heading", { name: "Direction" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Direction" })).toBeVisible();
    await expect(page.getByLabel("Rocks label")).toHaveCount(0);

    await api.setWorkspaceFeatureFlag("cerebro_operating_system", false);
    await page.reload();
    await expect(page.getByRole("link", { name: "Direction" })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Priorities" })).toHaveCount(0);
    await page.goto(`/${slug}/settings`);
    await expect(page.getByRole("tab", { name: "Operating System" })).toHaveCount(0);

    await api.setWorkspaceFeatureFlag("cerebro_operating_system", true);
    await page.reload();
    await expect(page.getByRole("tab", { name: "Operating System" })).toBeVisible();
  });
});

function request(api: TestApiClient, path: string, init: RequestInit = {}) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${api.getToken()}`,
      "X-Workspace-Slug": api.getWorkspaceSlug(),
      ...init.headers,
    },
  });
}
