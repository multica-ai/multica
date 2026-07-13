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
        if (item.title === strategyTitle) await request(api, `/api/cerebro/strategy-items/${item.id}`, { method: "DELETE" });
      }
    }
    await api.cleanup();
  });

  test("plans a horizon, connects a Project Rock, updates Issue health, and changes terminology without Atlas", async ({ page }) => {
    const requests: string[] = [];
    page.on("request", (request) => requests.push(request.url()));
    const project = await api.createProject(`E2E Rock ${Date.now()}`);

    await page.goto(`/${slug}/rocks`);
    await page.getByRole("button", { name: "Add Rock" }).click();
    await page.getByLabel("Project").selectOption(project.id);
    await page.getByLabel("Period start").fill("2026-07-01");
    await page.getByLabel("Period end").fill("2026-09-30");
    await page.getByLabel("Confidence").fill("80");
    await page.getByLabel("Reported health").selectOption("on_track");
    await page.getByRole("button", { name: "Save Rock" }).click();
    await expect(page.getByRole("heading", { name: project.title })).toBeVisible();

    await api.createIssue(`E2E blocked ${Date.now()}`, { project_id: project.id, status: "blocked" });
    await api.createIssue(`E2E done ${Date.now()}`, { project_id: project.id, status: "done" });
    await page.reload();
    await expect(page.getByText("1 of 2 issues done")).toBeVisible();
    await expect(page.getByText("1 blocked")).toBeVisible();
    await expect(page.getByRole("button", { name: "Why off track?" })).toBeVisible();

    await page.goto(`/${slug}/strategy`);
    await page.getByRole("button", { name: "Add Strategy item" }).click();
    await page.getByLabel("Type").selectOption("horizon_goal");
    await page.getByLabel("Title").fill(strategyTitle);
    await page.getByLabel("Horizon count").fill("18");
    await page.getByLabel("Horizon unit").selectOption("month");
    await page.getByLabel("Connected Rock").selectOption(project.id);
    await page.getByRole("button", { name: "Save Strategy item" }).click();
    await expect(page.getByText(strategyTitle)).toBeVisible();
    await expect(page.getByText("18 months")).toBeVisible();

    await page.getByRole("button", { name: "Customize labels" }).click();
    await page.getByLabel("Strategy label").fill("Direction");
    await page.getByLabel("Rock label").fill("Priority");
    await page.getByLabel("Rocks label").fill("Priorities");
    await page.getByRole("button", { name: "Save labels" }).click();
    await expect(page.getByRole("heading", { name: "Direction" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Connected Priorities" })).toBeVisible();
    expect(requests.some((url) => url.toLowerCase().includes("atlas"))).toBe(false);
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
              name: route === "rocks" ? "Add Rock" : "Add Strategy item",
            }),
          ).toBeVisible();
          await page.screenshot({
            path: testInfo.outputPath(`${route}-${viewport.name}.png`),
            fullPage: true,
          });
        }
      }
    },
  );
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
