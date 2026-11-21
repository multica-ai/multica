import { expect, test } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test("keeps the Skills intro actions clickable after paging the carousel", async ({ page }) => {
  await page.setViewportSize({ width: 1318, height: 870 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const slug = await loginAsDefault(page);
  await page.route("**/api/skills**", (route) => {
    const request = route.request();
    if (request.method() !== "GET" || !new URL(request.url()).pathname.endsWith("/api/skills")) {
      return route.fallback();
    }
    return route.fulfill({ json: [] });
  });
  await page.goto(`/${slug}/skills`, { waitUntil: "domcontentloaded" });

  const intro = page.getByRole("region", { name: "Turn how your team works into skills" });
  const create = intro.getByRole("button", { name: "New skill", exact: true });
  const docs = intro.getByRole("link", { name: "Read the docs" });

  for (const slide of [1, 2, 3]) {
    await intro.getByRole("button", { name: `Go to slide ${slide}` }).click();
    // The transparent carousel track used to intercept clicks over the actions
    // on the last slide. Browser hit testing is required; jsdom has no layout.
    await expect.poll(() => docs.evaluate((link) => {
      const rect = link.getBoundingClientRect();
      return link.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2));
    })).toBe(true);
    await create.click({ timeout: 3000 });
    await page.getByRole("menuitem", { name: /^Create manually/ }).click();
    await expect(page.getByRole("dialog", { name: "Create manually" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }
});
