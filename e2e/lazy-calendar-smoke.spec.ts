import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import { mkdirSync } from "node:fs";

test("calendar lazy loading in issue details, custom properties and date-range filters", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const api = await createTestApi();
  try {
    await api.createProperty("Review date", "date");
    const issue = await api.createIssue("Calendar loading verification", { due_date: "2026-11-05" });
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`);
    const sidebar = page.locator("[data-right-sidebar-panel]");
    const date = sidebar.getByRole("button", { name: "Nov 5", exact: true });
    await date.click();
    await expect(page.getByRole("grid")).toBeVisible({ timeout: 15_000 });
    if (process.env.SHOTS_DIR) {
      mkdirSync(process.env.SHOTS_DIR, { recursive: true });
      await page.screenshot({ path: `${process.env.SHOTS_DIR}/calendar-after.png`, animations: "disabled" });
    }
    const target = new Date(2026, 10, 6).toLocaleDateString("en-US");
    await page.locator(`button[data-day="${target}"]`).click();
    await expect(page.getByRole("grid")).toBeHidden();
    await expect(sidebar.getByRole("button", { name: "Nov 6", exact: true })).toBeVisible();
    await sidebar.getByRole("button", { name: "Nov 6", exact: true }).click();
    await page.getByRole("button", { name: "No due date", exact: true }).click();
    await expect(page.getByRole("grid")).toBeHidden();

    await sidebar.getByRole("button", { name: "Add property", exact: true }).click();
    await page.getByTestId("add-property-popover").getByRole("button", { name: "Review date", exact: true }).click();
    await expect(page.getByRole("grid")).toBeVisible();
    const today = await page.evaluate(() => new Date().toLocaleDateString());
    await page.locator(`button[data-day="${today}"]`).click();
    await expect(page.getByRole("grid")).toBeHidden();

    await page.goto(`/${slug}/issues`);
    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await page.getByRole("menuitem", { name: /^Date\b/ }).hover();
    await page.getByRole("button", { name: "Custom date or range", exact: true }).click();
    await expect(page.getByRole("grid")).toBeVisible();
    await expect(page.locator("button[data-day]:focus")).toHaveCount(1);
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Enter");
    await expect(page.getByRole("button", { name: "Apply", exact: true })).toBeEnabled();
    if (process.env.SHOTS_DIR) {
      await page.screenshot({ path: `${process.env.SHOTS_DIR}/range-after.png`, animations: "disabled" });
    }
    await page.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(page.getByRole("grid")).toBeHidden();
  } finally {
    await api.cleanup();
  }
});
