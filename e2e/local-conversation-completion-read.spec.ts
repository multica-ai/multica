import { expect, test } from "@playwright/test";
import "./env";

declare global {
  interface Window { completionFixture: { finish(): Promise<void>; flushed(): Promise<void> } }
}

for (const theme of ["light", "dark"]) test(`restores the ${theme} blue dot and clears it only after seeing the result`, async ({ page }) => {
  await page.goto("/tutti-agent-gui/completion-read.html");
  await page.evaluate((value) => { document.documentElement.classList.toggle("dark", value === "dark"); }, theme);
  const dot = page.getByRole("status", { name: "Unread", exact: true });
  await expect(page.getByRole("link", { name: /Background conversation/ })).toBeVisible();
  await expect(dot).toHaveCount(0);
  await page.screenshot({ path: `.tmp/completion-read/${theme}-before.png` });
  await page.evaluate(() => window.completionFixture.finish());
  await expect(dot).toBeVisible();
  await page.screenshot({ path: `.tmp/completion-read/${theme}-after.png` });
  await page.reload();
  await expect(dot).toBeVisible();
  await page.locator("#open").click();
  await expect(dot).toBeVisible();
  await page.locator('[data-agent-transcript-row="answer"]').scrollIntoViewIfNeeded();
  await expect(dot).toHaveCount(0);
  await page.evaluate(() => window.completionFixture.flushed());
  await page.reload();
  await expect(page.getByRole("link", { name: /Background conversation/ })).toBeVisible();
  await expect(dot).toHaveCount(0);
});

test("recovers a turn completed while closed", async ({ page }) => {
  await page.goto("/tutti-agent-gui/completion-read.html");
  await expect(page.getByRole("link", { name: /Background conversation/ })).toBeVisible();
  await page.evaluate(() => sessionStorage.setItem("completed", "true"));
  await page.reload();
  const dot = page.getByRole("status", { name: "Unread", exact: true });
  await expect(dot).toBeVisible();
});
