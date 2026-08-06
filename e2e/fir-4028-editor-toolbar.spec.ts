import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("FIR-4028 — configurable editor toolbar", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async ({ request }) => {
    const token = api.getToken();
    if (token) {
      await request.patch("/api/me/preferences", {
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        data: { cerebro_editor_toolbar_order: null },
      });
    }
    await api.cleanup();
  });

  test("the chosen order survives reload and appears above Notes and Documents without a selection", async ({
    page,
  }) => {
    const document = await api.createAgentDocument(
      `Toolbar document ${Date.now()}`,
      "Document body",
    );
    const note = await api.createSharedNote(
      `Toolbar note ${Date.now()}`,
      "Note body",
    );

    await page.goto(`/${workspaceSlug}/settings`);
    await page.getByRole("tab", { name: "Notes", exact: true }).click();
    await expect(page.getByText("Formatting toolbar")).toBeVisible();

    await page.getByRole("button", { name: "Reset" }).click();
    await expect(
      page.getByText("Formatting toolbar updated").last(),
    ).toBeVisible();
    await page.getByRole("button", { name: "Move Link up" }).click();
    await expect(
      page.getByText("Formatting toolbar updated").last(),
    ).toBeVisible();

    await page.reload();
    await page.getByRole("tab", { name: "Notes", exact: true }).click();
    const linkSetting = page.getByTestId("toolbar-setting-link");
    await expect(linkSetting).toBeVisible();
    await expect(page.getByTestId("toolbar-setting-bold")).toBeVisible();
    expect(
      await page.locator('[data-testid^="toolbar-setting-"]').evaluateAll(
        (rows) =>
          rows.indexOf(
            document.querySelector('[data-testid="toolbar-setting-link"]')!,
          ) <
          rows.indexOf(
            document.querySelector('[data-testid="toolbar-setting-bold"]')!,
          ),
      ),
    ).toBe(true);

    for (const path of [
      `/${workspaceSlug}/documents/${document.id}`,
      `/${workspaceSlug}/notes/${note.id}`,
    ]) {
      await page.goto(path);
      const toolbar = page.getByRole("toolbar", {
        name: "Formatting toolbar",
      });
      await expect(toolbar).toBeVisible({ timeout: 15000 });
      await expect(toolbar.getByRole("button", { name: "Code" })).toBeVisible();
      await expect(toolbar.getByRole("button", { name: "Link" })).toBeVisible();
      await expect(toolbar.getByRole("button", { name: "Bold" })).toBeVisible();
      expect(
        await toolbar.getByRole("button").evaluateAll(
          (buttons) =>
            buttons.findIndex(
              (button) => button.getAttribute("aria-label") === "Link",
            ) <
            buttons.findIndex(
              (button) => button.getAttribute("aria-label") === "Bold",
            ),
        ),
      ).toBe(true);
      expect(
        await page.evaluate(() => window.getSelection()?.isCollapsed ?? true),
      ).toBe(true);
    }
  });
});
