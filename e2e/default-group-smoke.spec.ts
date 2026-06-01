import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Default group (FIR-2732)", () => {
  let api: TestApiClient;

  test.afterEach(async () => {
    if (api) await api.cleanup?.();
  });

  test("Auth & Permissions tab renders with standard-gruppe picker", async ({
    page,
  }) => {
    const slug = await loginAsDefault(page);

    await page.goto(`/${slug}/settings?tab=auth-permissions`, {
      waitUntil: "domcontentloaded",
    });
    await page.waitForURL(`**/${slug}/settings**`, { timeout: 30000 });

    await expect(page.getByTestId("auth-permissions-tab")).toBeVisible({
      timeout: 30000,
    });
    await expect(
      page.getByText("Standard-gruppe for nye brugere"),
    ).toBeVisible();
  });

  test("dropdown shows group NAME (not UUID) after save and reload", async ({
    page,
  }) => {
    api = await createTestApi();
    const wsId = api.getWorkspaceId();
    const slug = api.getWorkspaceSlug();
    if (!wsId || !slug) throw new Error("workspace not initialised");

    // Ensure at least one selectable group exists, then pin it as the
    // workspace default via the auth-settings API. Reload after save and
    // assert the trigger renders the group NAME, not the raw UUID — the
    // bug Tine reported (#854 QA round 2) showed the dropdown displaying
    // "6de8ffd6-…" instead of the group name after refresh.
    const groupName = `Default Smoke ${Date.now()}`;
    const group = await api.createCerebroGroup(groupName);
    await api.setAuthSettingsDefaultGroup(group.id);

    await loginAsDefault(page);
    await page.goto(`/${slug}/settings?tab=auth-permissions`, {
      waitUntil: "domcontentloaded",
    });
    await expect(page.getByTestId("auth-permissions-tab")).toBeVisible({
      timeout: 30000,
    });

    const tab = page.getByTestId("auth-permissions-tab");
    const trigger = tab
      .locator('section', { hasText: "Standard-gruppe for nye brugere" })
      .locator('[data-slot="select-value"]');
    await expect(trigger).toHaveText(groupName);
    await expect(trigger).not.toHaveText(group.id);
  });

  test("member profile route resolves (no 404) for current user", async ({
    page,
  }) => {
    // Round-2 QA reported an intermittent "Page not found" on the member
    // profile route. The page file exists; this guard prevents regressions
    // by navigating to the current user's own member-detail URL and
    // asserting the profile shell renders instead of the global 404.
    api = await createTestApi();
    const slug = api.getWorkspaceSlug();
    const userId = api.getUserId();
    if (!slug || !userId) throw new Error("workspace/user not initialised");

    await loginAsDefault(page);
    await page.goto(`/${slug}/members/${userId}`, {
      waitUntil: "domcontentloaded",
    });

    await expect(page.getByText("Page not found")).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: /back to members/i }),
    ).toBeVisible({ timeout: 30000 });
  });
});
