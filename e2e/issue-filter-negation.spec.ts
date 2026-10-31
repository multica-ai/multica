import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// "Is not" flips one dimension to its complement. The list surface is
// server-driven, so this exercises menu → store → table query → SQL predicate.

async function visibleIssueTitles(page: Page, titles: string[]) {
  const present: string[] = [];
  for (const title of titles) {
    if (await page.getByText(title, { exact: true }).first().isVisible()) {
      present.push(title);
    }
  }
  return present.sort();
}

async function closeMenus(page: Page) {
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
}

test.describe("Issue filter: is not", () => {
  let api: TestApiClient;
  const suffix = Date.now().toString(36);
  const high = `negate high ${suffix}`;
  const low = `negate low ${suffix}`;
  const none = `negate none ${suffix}`;
  const all = [high, low, none];

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.createIssue(high, { priority: "high" });
    await api.createIssue(low, { priority: "low" });
    await api.createIssue(none);
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("excludes the selected values and keeps everything else", async ({ page }) => {
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());

    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await page.getByRole("menuitem", { name: "Priority" }).click();
    await page.getByRole("menuitemcheckbox", { name: "High" }).click();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([high]);

    // Flipping the operator keeps the selection open and inverts it, issues
    // without a priority included.
    await page.getByRole("menuitemradio", { name: "is not" }).click();
    await expect(page.getByRole("menuitemcheckbox", { name: "High" })).toBeVisible();
    await closeMenus(page);
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([low, none].sort());

    // The dimension row reads as a sentence: "Priority is not High".
    await page.getByRole("button", { name: "1 filter" }).click();
    const row = page.getByRole("menuitem", { name: /^Priority\s*is not\s*High/ });
    await expect(row).toBeVisible();
    // An Escape that lands while the menu is still opening is dropped.
    await expect(async () => {
      await page.keyboard.press("Escape");
      await expect(row).toBeHidden({ timeout: 1000 });
    }).toPass();

    await page.getByRole("button", { name: "Reset" }).click();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());
  });
});
