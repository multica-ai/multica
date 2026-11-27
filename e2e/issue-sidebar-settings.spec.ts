import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";

for (const height of [420, 900]) {
  test(`sidebar settings contain switch hit areas at ${height}px height`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height });
    const api = await createTestApi();
    try {
      const issue = await api.createIssue("Sidebar settings overflow regression");
      const slug = await loginAsDefault(page);
      await page.goto(`/${slug}/issues/${issue.identifier}`, { waitUntil: "domcontentloaded" });
      await waitForPageText(page, issue.title);
      await page.getByRole("button", { name: "Customize sidebar", exact: true }).click();
      const settings = page.getByRole("dialog");
      const list = settings.locator("ul");
      await expect(list).toBeVisible();
      await expect.poll(() => list.evaluate((el) => el.scrollWidth - el.clientWidth)).toBe(0);

      if (height === 420) {
        await list.hover();
        await page.mouse.wheel(0, 1000);
        await expect.poll(() => list.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
      }
      const toggle = settings.getByRole("switch", { name: "Metadata", exact: true });
      await toggle.scrollIntoViewIfNeeded();
      await toggle.focus();
      await page.keyboard.press("Space");
      await expect(toggle).not.toBeChecked();
      await settings.getByRole("button", { name: "Reset", exact: true }).click();
      await expect(toggle).toBeChecked();
      await page.keyboard.press("Escape");
      await expect(settings).toBeHidden();
    } finally {
      await api.cleanup();
    }
  });
}
