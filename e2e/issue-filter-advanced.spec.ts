import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// Menu search, the due-date dimension and advanced mode, end to end: menu or
// panel → store → table query → SQL. SHOTS_DIR, when set, collects screenshots.

async function visibleIssueTitles(page: Page, titles: string[]) {
  const present: string[] = [];
  for (const title of titles) {
    if (await page.getByText(title, { exact: true }).first().isVisible()) {
      present.push(title);
    }
  }
  return present.sort();
}

async function shot(page: Page, name: string) {
  const dir = process.env.SHOTS_DIR;
  if (!dir) return;
  // Let menus and tooltips finish their enter/exit transitions.
  await page.waitForTimeout(400);
  await page.screenshot({ path: `${dir}/${name}.png` });
}

async function closeMenus(page: Page) {
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
}

function localDate(offsetDays: number) {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

test.describe("Issue filter: search, due date, advanced", () => {
  let api: TestApiClient;
  const suffix = Date.now().toString(36);
  const urgentLate = `adv urgent late ${suffix}`;
  const urgentUndated = `adv urgent undated ${suffix}`;
  const lowLate = `adv low late ${suffix}`;
  const lowUndated = `adv low undated ${suffix}`;
  const mediumLate = `adv medium late ${suffix}`;
  const all = [urgentLate, urgentUndated, lowLate, lowUndated, mediumLate];

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const yesterday = localDate(-1);
    await api.createIssue(urgentLate, { priority: "urgent", due_date: yesterday });
    await api.createIssue(urgentUndated, { priority: "urgent" });
    await api.createIssue(lowLate, { priority: "low", due_date: yesterday });
    await api.createIssue(lowUndated, { priority: "low" });
    await api.createIssue(mediumLate, { priority: "medium", due_date: yesterday });
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("search adds a value as a condition; due date filters by bucket", async ({ page }) => {
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());

    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await page.getByRole("textbox", { name: "Filter by field or value…" }).fill("urg");
    const urgentRow = page.getByRole("menuitemcheckbox", { name: /Priority\s*is\s*Urgent/ });
    await expect(urgentRow).toBeVisible();
    await shot(page, "01-search");
    await urgentRow.click();
    await closeMenus(page);
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([urgentLate, urgentUndated].sort());

    await page.getByRole("button", { name: "1 filter" }).click();
    await page.getByRole("menuitem", { name: /^Due date/ }).click();
    await page.getByRole("menuitemcheckbox", { name: "Overdue" }).click();
    await shot(page, "02-due-date");
    await closeMenus(page);
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([urgentLate]);
  });

  test("advanced mode ORs inside a group and switches back only when simple can say it", async ({ page }) => {
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());

    // Start from a simple filter: due date is overdue.
    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await page.getByRole("menuitem", { name: /^Due date/ }).click();
    await page.getByRole("menuitemcheckbox", { name: "Overdue" }).click();
    await closeMenus(page);
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([lowLate, mediumLate, urgentLate].sort());

    // Switching carries it over as the first rule.
    await page.getByRole("button", { name: "1 filter" }).click();
    await page.getByRole("menuitem", { name: /Switch to advanced filter/ }).click();
    const panel = page.getByTestId("advanced-filter-panel");
    await expect(panel).toBeVisible();
    await expect(panel.getByTestId("advanced-filter-row")).toHaveCount(1);
    await expect(panel.getByRole("button", { name: "Switch to simple filter" })).toBeEnabled();

    // AND (priority is urgent OR priority is low), as a group.
    await panel.getByRole("button", { name: "Add condition group" }).click();
    await page.getByRole("menuitem", { name: "Priority" }).click();
    const group = panel.getByTestId("advanced-filter-group");
    await expect(group).toBeVisible();
    await group.getByRole("button", { name: "Values" }).first().click();
    await page.getByRole("menuitemcheckbox", { name: "Urgent" }).click();
    await page.keyboard.press("Escape");
    await group.getByRole("button", { name: "Add condition" }).click();
    await page.getByRole("menuitem", { name: "Priority" }).click();
    await group.getByRole("button", { name: "Values" }).nth(1).click();
    await page.getByRole("menuitemcheckbox", { name: "Low" }).click();
    await page.keyboard.press("Escape");

    await expect(panel).toBeVisible();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([lowLate, urgentLate].sort());
    await expect(panel.getByRole("button", { name: "Switch to simple filter" })).toBeDisabled();
    await expect(panel.getByText(/Simple filters can't show groups/)).toBeVisible();
    await shot(page, "03-advanced");

    // The trigger names the mode; the chip bar carries the tree as one chip.
    await page.keyboard.press("Escape");
    await expect(panel).toBeHidden();
    await expect(page.getByRole("button", { name: "Advanced filter" })).toBeVisible();
    await page.mouse.move(600, 400);
    await shot(page, "04-advanced-closed");

    // Delete the group: one overdue rule is a simple filter again.
    await page.getByRole("button", { name: "Advanced filter" }).click();
    await expect(panel).toBeVisible();
    await group.getByRole("button", { name: "Condition actions" }).last().click();
    await page.getByRole("menuitem", { name: "Delete group" }).click();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([lowLate, mediumLate, urgentLate].sort());
    await panel.getByRole("button", { name: "Switch to simple filter" }).click();
    await expect(panel).toBeHidden();
    await expect(page.getByRole("button", { name: "1 filter" })).toBeVisible();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([lowLate, mediumLate, urgentLate].sort());
  });

  test("custom properties are conditions like any other field", async ({ page }) => {
    const property = await api.createProperty(`Estimate ${suffix}`, "number");
    await page.reload();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());

    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await page.getByRole("menuitem", { name: /Switch to advanced filter/ }).click();
    const panel = page.getByTestId("advanced-filter-panel");
    await panel.getByRole("button", { name: "Add condition", exact: true }).click();
    await page.getByRole("menuitem", { name: "Priority" }).click();
    await panel.getByRole("button", { name: "Values" }).first().click();
    await page.getByRole("menuitemcheckbox", { name: "Urgent" }).click();
    await page.keyboard.press("Escape");

    await panel.getByRole("button", { name: "Add condition", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: property.name })).toBeVisible();
    await shot(page, "05-advanced-field-picker");
    await page.getByRole("menuitem", { name: property.name }).click();
    await panel.getByRole("button", { name: "Values" }).nth(1).click();
    await page.getByRole("menuitemcheckbox", { name: "No value" }).click();
    await page.keyboard.press("Escape");

    const rows = panel.getByTestId("advanced-filter-row");
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(1)).toContainText(property.name);
    await expect(panel.getByRole("button", { name: "Add property filter" })).toHaveCount(0);
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([urgentLate, urgentUndated].sort());
    await shot(page, "06-advanced-properties");

    await panel.getByRole("button", { name: "Clear all" }).click();
    await expect(rows).toHaveCount(0);
    await expect(panel.getByRole("button", { name: "Clear all" })).toBeDisabled();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([...all].sort());
  });
});
