import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

test.describe("Cerebro operating system", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_operating_system", true);
    await request(api, "/api/cerebro/operating-system/settings", {
      method: "PUT",
      body: JSON.stringify({ terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" } }),
    });
    slug = await loginAsDefault(page);
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

  test("creates and edits a standalone Rock without a Strategy connection", async ({ page }, testInfo) => {
    const rockErrors: string[] = [];
    page.on("response", (response) => {
      if (response.status() >= 400 && response.url().includes("/api/cerebro/rocks")) {
        rockErrors.push(`${response.status()} ${response.url()}`);
      }
    });
    const suffix = Date.now();
    await page.goto(`/${slug}/rocks`);
    await page.getByRole("button", { name: "List view" }).click();
    const originalTitle = `E2E v4 Rock ${suffix}`;
    await page.getByLabel("New Rock title").fill(originalTitle);
    await page.getByLabel("New Rock title").press("Enter");
    await expect(page.getByRole("button", { name: `Edit ${originalTitle}` })).toBeVisible();

    await page.getByRole("button", { name: `Edit ${originalTitle}` }).click();
    await expect(page.getByRole("heading", { name: `Edit ${originalTitle}` })).toBeVisible();
    await expect(page.getByLabel("Strategy connection")).toContainText("No Strategy connection");
    const editedTitle = `E2E v4 Rock edited ${suffix}`;
    await page.getByLabel("Rock title", { exact: true }).fill(editedTitle);
    await page.getByRole("button", { name: "Save Rock" }).click();
    await expect(page.getByRole("button", { name: `View ${editedTitle}` })).toBeVisible();

    await page.reload();
    await page.getByRole("button", { name: "List view" }).click();
    const persistedRock = page.getByRole("button", { name: `Edit ${editedTitle}` });
    await expect(persistedRock).toBeVisible();
    await persistedRock.click();
    await expect(page.getByLabel("Strategy connection")).toContainText("No Strategy connection");
    await testInfo.attach("standalone-rock-saved", {
      body: await page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });
    expect(rockErrors).toEqual([]);
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
              name: route === "rocks" ? "List view" : "Add page",
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
      await page.getByRole("button", { name: "List view" }).click();
      const newRockTitle = page.getByLabel("New Rock title");
      await newRockTitle.focus();
      await expect(newRockTitle).toBeFocused();
      const keyboardTitle = `E2E v4 Rock keyboard ${Date.now()}`;
      await newRockTitle.fill(keyboardTitle);
      await page.keyboard.press("Enter");
      await expect(page.getByRole("button", { name: `Edit ${keyboardTitle}` })).toBeVisible();
    },
  );

  test("keeps terminology in Settings and gates routes and navigation with the feature flag", async ({ page }) => {
    await page.goto(`/${slug}/settings`);
    await page.getByRole("tab", { name: "Operating System" }).click();
    await page.getByRole("combobox", { name: "Profile" }).selectOption("custom");
    await page.getByLabel("Strategy label").fill("Direction");
    await page.getByLabel("Goal label (singular)").fill("Priority");
    await page.getByLabel("Goals label (plural)").fill("Priorities");
    await page.getByRole("button", { name: "Save terminology" }).click();

    await page.goto(`/${slug}/rocks`);
    await expect(page.getByRole("heading", { name: "Priorities", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Priorities" })).toBeVisible();
    await expect(page.getByLabel("Strategy label")).toHaveCount(0);
    await page.goto(`/${slug}/strategy`);
    await expect(page.getByRole("heading", { name: "Strategy Map" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Direction" })).toBeVisible();
    await expect(page.getByLabel("Goals label (plural)")).toHaveCount(0);

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
