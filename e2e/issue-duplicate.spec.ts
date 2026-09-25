import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// Mark as duplicate (MUL-7349): the status picker's action cancels an issue
// and links it to its original; the original lists it; reopening removes it.
test.describe("Mark as duplicate", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    slug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("marks, links both sides, and unmarks", async ({ page }) => {
    const run = Date.now().toString(36);
    const originalTitle = `Tap targets too small ${run}`;
    const original = await api.createIssue(originalTitle, { status: "in_progress" });
    const duplicateTitle = `Status picker hard to tap ${run}`;
    const duplicate = await api.createIssue(duplicateTitle, { status: "todo" });

    await page.goto(`/${slug}/issues/${duplicate.id}`);
    // The detail page can close a just-opened picker while it finishes
    // loading, so retry opening until the action takes the click.
    await expect(async () => {
      await page.getByRole("button", { name: "Todo", exact: true }).first().click();
      await page.getByRole("button", { name: "Mark as duplicate" }).click({ timeout: 2000 });
    }).toPass();

    const picker = page.getByRole("dialog");
    await picker.getByPlaceholder("Search issues...").fill(originalTitle);
    await picker.getByText(originalTitle).click();

    const originalLink = page.getByRole("link", {
      name: `${original.identifier} ${originalTitle}`,
    });
    await expect(originalLink).toBeVisible();
    await expect(page.getByRole("button", { name: "Cancelled", exact: true }).first()).toBeVisible();
    // The mark is logged, with the original linked by its bare identifier.
    await expect(page.getByText(`marked this issue as a duplicate of ${original.identifier}`)).toBeVisible();
    await expect(page.getByRole("link", { name: original.identifier, exact: true })).toBeVisible();

    await originalLink.click();
    await expect(page).toHaveURL(new RegExp(`/issues/${original.id}$`));
    await expect(page.getByRole("button", { name: "Duplicates" })).toBeVisible();
    // The sidebar row names the duplicate with its title; the activity feed
    // links it by identifier alone.
    await expect(page.getByRole("link", { name: `${duplicate.identifier} ${duplicateTitle}` })).toBeVisible();
    await expect(page.getByText(`marked ${duplicate.identifier} as a duplicate of this issue`)).toBeVisible();

    await page.goto(`/${slug}/issues/${duplicate.id}`);
    await page.getByRole("button", { name: "Not a duplicate" }).click();
    await expect(page.getByRole("button", { name: "Not a duplicate" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Todo", exact: true }).first()).toBeVisible();
    await expect(page.getByText(`unmarked this issue as a duplicate of ${original.identifier}`)).toBeVisible();
  });
});
