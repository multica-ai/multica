import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient | undefined;

test.beforeEach(async ({ page }) => {
  api = await createTestApi();
  await loginAsDefault(page);
});

test.afterEach(async () => {
  await api?.cleanup();
});

test("agent hover card shows settings, account status, and remaining usage", async ({
  page,
}, testInfo) => {
  const fixture = await api!.createAgentProfileFixture();

  await page.goto(`/e2e-workspace/runtimes/${fixture.runtimeId}`);
  await expect(
    page.getByRole("heading", { name: fixture.runtimeName }),
  ).toBeVisible();

  const agentRow = page.getByRole("link", { name: new RegExp(fixture.agentName) });
  await agentRow.locator('[data-slot="avatar"]').hover();

  const card = page.locator('[data-slot="hover-card-content"]');
  await expect(card).toBeVisible();
  await expect(card.getByText("claude-opus-5")).toBeVisible();
  await expect(card.getByText("high")).toBeVisible();
  await expect(card.getByText("3 tasks")).toBeVisible();
  await expect(card.getByText(fixture.identity)).toBeVisible();
  await expect(card.getByText("Available")).toBeVisible();

  const nextFiveHours = card.getByLabel("Next 5h usage");
  await expect(nextFiveHours.getByText("73% left")).toBeVisible();
  await expect(nextFiveHours.getByText("12.4k tok used")).toBeVisible();
  await expect(nextFiveHours.getByText(/^resets in /)).toBeVisible();

  const week = card.getByLabel("This week usage");
  await expect(week.getByText("41% left")).toBeVisible();
  await expect(week.getByText("1.8M tok used")).toBeVisible();

  await testInfo.attach("fir-3660-agent-profile-tooltip", {
    body: await card.screenshot(),
    contentType: "image/png",
  });
});
