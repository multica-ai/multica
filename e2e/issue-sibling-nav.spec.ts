import { test, expect, type Page } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * E2E: previous/next navigation from inside the issue detail.
 *
 * The board/list views publish the column the user is looking at; the detail
 * reads it back so you can step between sibling issues without backing out to
 * the board. Here we open a column, walk to the bottom (Next greys out), then
 * walk back to the top (Previous greys out), proving both the stepping and the
 * end-of-column disabled states.
 */
test.describe("Issue detail previous/next navigation", () => {
  let api: TestApiClient;
  let wsSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    wsSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  async function clickAndAwaitNavigation(page: Page, button: ReturnType<Page["getByRole"]>) {
    const before = page.url();
    await button.click();
    await page.waitForFunction((url) => location.href !== url, before);
  }

  test("steps through a column and greys out at the ends", async ({ page }) => {
    // Three issues in the same column so it has a clear top, middle and bottom.
    const stamp = Date.now();
    const titles = [
      `Sibling nav A ${stamp}`,
      `Sibling nav B ${stamp}`,
      `Sibling nav C ${stamp}`,
    ];
    for (const title of titles) {
      await api.createIssue(title, { status: "todo" });
    }
    await page.reload();

    // Open one of our issues from the board.
    const card = page
      .locator('a[href*="/issues/"]')
      .filter({ hasText: titles[1] });
    await expect(card).toBeVisible({ timeout: 10000 });
    await card.click();
    await page.waitForURL(/\/issues\/[\w-]+/);

    const prev = page.getByRole("button", { name: "Previous issue" });
    const next = page.getByRole("button", { name: "Next issue" });

    // Coming from the board, the column context exists, so both show.
    await expect(prev).toBeVisible();
    await expect(next).toBeVisible();

    // Walk to the bottom of the column. Each step must land on a new issue and
    // never revisit one, so navigation is strictly forward.
    const visited = new Set<string>([page.url()]);
    for (let i = 0; i < 50 && !(await next.isDisabled()); i++) {
      await clickAndAwaitNavigation(page, next);
      await expect(next).toBeVisible();
      expect(visited.has(page.url())).toBe(false);
      visited.add(page.url());
    }
    // At the last card there is nowhere further down.
    await expect(next).toBeDisabled();
    await expect(prev).toBeEnabled();

    // Walk back up to the top the same way.
    for (let i = 0; i < 50 && !(await prev.isDisabled()); i++) {
      await clickAndAwaitNavigation(page, prev);
      await expect(prev).toBeVisible();
    }
    // At the first card there is nowhere further up.
    await expect(prev).toBeDisabled();
    await expect(next).toBeEnabled();
  });

  test("hides the buttons when there is no column context (deep link)", async ({ page }) => {
    // An issue opened cold — never having visited a list this session — has no
    // siblings to offer, so the controls stay hidden rather than dangling.
    const issue = await api.createIssue(`Deep link ${Date.now()}`, { status: "todo" });
    // `page.goto` is a full document load, so the in-memory navigation store
    // starts empty here regardless of the board `loginAsDefault` left mounted —
    // no realtime publish from issue creation can leak into this assertion.
    await page.goto(`/${wsSlug}/issues/${issue.id}`);
    await page.waitForURL(/\/issues\/[\w-]+/);

    // The detail has loaded (its sidebar is present)...
    await expect(page.locator("text=Properties")).toBeVisible({ timeout: 10000 });
    // ...but with no list context, neither navigation button renders.
    await expect(page.getByRole("button", { name: "Previous issue" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Next issue" })).toHaveCount(0);
  });
});
