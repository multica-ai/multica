import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * FIR-4028 slice 10 — the keyboard-docked phone row, in a browser.
 *
 * The row only exists behind `(pointer: coarse) and (max-width: 767px)`. The
 * default Playwright project runs a fine pointer, so every other phone-width
 * test in this repo exercises the desktop overflow menu and never reaches
 * `FormatSheet`, the docked row or the keyboard inset. `isMobile` is what makes
 * Chromium report a coarse primary pointer — `hasTouch` alone does not.
 */
test.use({
  viewport: { width: 390, height: 844 },
  hasTouch: true,
  isMobile: true,
});

test.describe("FIR-4028 — the phone toolbar under a coarse pointer", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", true);
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);
    await api.cleanup();
  });

  test("the emulated context really is a phone", async ({ page }) => {
    // The gate for everything below. If this is false the other tests here are
    // silently testing the desktop row and proving nothing about slice 10.
    await page.goto(`/${workspaceSlug}/notes`);
    expect(
      await page.evaluate(
        () =>
          window.matchMedia("(pointer: coarse) and (max-width: 767px)").matches,
      ),
    ).toBe(true);
  });

  test("the row is absent until the editor has focus, then it is docked to the bottom", async ({
    page,
  }) => {
    const note = await api.createSharedNote(
      `Phone toolbar note ${Date.now()}`,
      "Format this sentence on a phone.",
    );
    await page.goto(`/${workspaceSlug}/notes/${note.id}`);

    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });

    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    // Reading a note costs no height: with no keyboard there is no row.
    await expect(toolbar).toHaveCount(0);

    await body.getByText("Format this sentence").tap();
    await expect(toolbar).toBeVisible();

    const docked = await toolbar.evaluate((row) => {
      const style = getComputedStyle(row);
      const box = row.getBoundingClientRect();
      return {
        position: style.position,
        bottomGap: window.innerHeight - box.bottom,
        spansWidth: Math.round(box.width) === window.innerWidth,
      };
    });
    expect(docked.position).toBe("fixed");
    expect(docked.spansWidth).toBe(true);
    // No keyboard in headless Chromium, so the inset is 0 and the row sits on
    // the bottom edge. The inset itself is driven in the next test.
    expect(Math.abs(docked.bottomGap)).toBeLessThanOrEqual(1);
  });

  test("the row rides the keyboard's top edge when the visual viewport shrinks", async ({
    page,
  }) => {
    const note = await api.createSharedNote(
      `Phone keyboard note ${Date.now()}`,
      "Format this sentence on a phone.",
    );
    await page.goto(`/${workspaceSlug}/notes/${note.id}`);

    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });
    await body.getByText("Format this sentence").tap();

    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    await expect(toolbar).toBeVisible();

    // A headless browser has no software keyboard, so the only honest way to
    // exercise the dock is the seam the implementation listens on: the visual
    // viewport shrinks by the keyboard's height and fires `resize`.
    const keyboardHeight = 336;
    await page.evaluate((height) => {
      const viewport = window.visualViewport;
      if (!viewport) throw new Error("visualViewport missing");
      Object.defineProperty(viewport, "height", {
        configurable: true,
        get: () => window.innerHeight - height,
      });
      viewport.dispatchEvent(new Event("resize"));
    }, keyboardHeight);

    await expect
      .poll(async () =>
        toolbar.evaluate(
          (row) => window.innerHeight - row.getBoundingClientRect().bottom,
        ),
      )
      .toBeGreaterThanOrEqual(keyboardHeight - 1);
  });

  test("the format sheet formats without dismissing the keyboard", async ({
    page,
  }) => {
    const note = await api.createSharedNote(
      `Phone sheet note ${Date.now()}`,
      "Format this sentence on a phone.",
    );
    await page.goto(`/${workspaceSlug}/notes/${note.id}`);

    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });
    await body.getByText("Format this sentence").tap();

    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    await expect(toolbar).toBeVisible();

    // Select a word to format. The first tap gave ProseMirror focus.
    await body.getByText("Format this sentence").dblclick();

    await toolbar.getByRole("button", { name: "More formatting" }).tap();
    const sheetItem = page.locator('[data-overflow-action="code"]');
    await expect(sheetItem).toBeVisible();
    await sheetItem.tap();

    // The whole reason the sheet is not a Sheet or a DropdownMenu: taking focus
    // on a phone closes the keyboard, so the panel that formats the text would
    // close the text. The row surviving proves focus never left the editor.
    await expect(toolbar).toBeVisible();
    await expect(body.locator("code")).toHaveCount(1);
  });

  test("the outline button does not sit on top of the docked row", async ({
    page,
  }) => {
    const note = await api.createSharedNote(
      `Phone outline note ${Date.now()}`,
      "Format this sentence on a phone.",
    );
    await page.goto(`/${workspaceSlug}/notes/${note.id}`);

    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });

    const outline = page.getByRole("button", { name: "Open outline" });
    await expect(outline).toBeVisible();

    await body.getByText("Format this sentence").tap();
    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    await expect(toolbar).toBeVisible();

    // Both are `fixed` at the bottom with the same stacking order. The row
    // spans the full width, so an outline button that keeps its own offset
    // lands on top of the row's right end.
    // The button carries `transition-all`, so it slides to its new offset
    // instead of jumping — a single read lands mid-flight.
    await expect
      .poll(async () =>
        toolbar.evaluate((row) => {
          const button = document.querySelector('[aria-label="Open outline"]');
          if (!button) return null;
          const a = row.getBoundingClientRect();
          const b = button.getBoundingClientRect();
          const vertical = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
          const horizontal =
            Math.min(a.right, b.right) - Math.max(a.left, b.left);
          return vertical > 0 && horizontal > 0;
        }),
      )
      .toBe(false);
  });

  test("nothing sits outside the row at phone width", async ({ page }) => {
    const note = await api.createSharedNote(
      `Phone reach note ${Date.now()}`,
      "Format this sentence on a phone.",
    );
    await page.goto(`/${workspaceSlug}/notes/${note.id}`);

    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });
    await body.getByText("Format this sentence").tap();

    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    await expect(toolbar).toBeVisible();

    const outside = await toolbar.evaluate((row) => {
      const rowBox = row.getBoundingClientRect();
      return Array.from(row.querySelectorAll("button"))
        .map((button) => ({
          label: button.getAttribute("aria-label") ?? "",
          box: button.getBoundingClientRect(),
        }))
        .filter(({ box }) => box.width > 0 && box.height > 0)
        .filter(({ box }) => box.left < rowBox.left || box.right > rowBox.right);
    });
    expect(outside.map(({ label }) => label)).toEqual([]);

    // A coarse pointer needs a target it can actually hit. 38px is the height
    // the sheet's own rows use; the docked row must not undercut it.
    const tooSmall = await toolbar.evaluate((row) =>
      Array.from(row.querySelectorAll("button"))
        .map((button) => ({
          label: button.getAttribute("aria-label") ?? "",
          box: button.getBoundingClientRect(),
        }))
        .filter(({ box }) => box.width > 0 && box.height > 0)
        .filter(({ box }) => box.height < 28 || box.width < 28)
        .map(({ label }) => label),
    );
    expect(tooSmall).toEqual([]);
  });
});
