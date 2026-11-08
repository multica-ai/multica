import { test, expect, type Page } from "@playwright/test";
import { loginAsDefault, reloadAppPage } from "./helpers";

const railOrder = (page: Page) =>
  page
    .locator("[data-slot='app-rail'] [data-rail-item]")
    .evaluateAll((items) => items.map((item) => (item as HTMLElement).dataset.railItem));

async function dragOnto(page: Page, from: string, to: string) {
  const rail = page.locator("[data-slot='app-rail']");
  const source = await rail.locator(`[data-rail-item='${from}']`).boundingBox();
  const target = await rail.locator(`[data-rail-item='${to}']`).boundingBox();
  if (!source || !target) throw new Error("rail item is not on screen");
  await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + source.width / 2, target.y + target.height / 2, { steps: 12 });
  await page.mouse.move(source.x + source.width / 2, target.y + target.height / 2 + 2, { steps: 2 });
  await page.mouse.up();
}

test("rail items reorder by drag and keep their order", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const slug = await loginAsDefault(page);
  await page.evaluate(() => localStorage.removeItem("multica_rail_order"));
  await reloadAppPage(page);

  expect(await railOrder(page)).toEqual(["new_issue", "search", "inbox", "work", "team", "chat", "autopilots"]);

  // Dropping Chat on Search must not also open Chat.
  await dragOnto(page, "chat", "search");
  expect(await railOrder(page)).toEqual(["new_issue", "chat", "search", "inbox", "work", "team", "autopilots"]);
  await expect(page).toHaveURL(new RegExp(`/${slug}/issues`));

  await dragOnto(page, "search", "autopilots");
  expect(await railOrder(page)).toEqual(["new_issue", "chat", "inbox", "work", "team", "autopilots", "search"]);
  await expect(page.getByRole("dialog")).toHaveCount(0);

  await reloadAppPage(page);
  expect(await railOrder(page)).toEqual(["new_issue", "chat", "inbox", "work", "team", "autopilots", "search"]);

  // A plain click after a drag still works.
  await page.locator("[data-rail-item='chat'] a").click();
  await expect(page).toHaveURL(new RegExp(`/${slug}/chat`));
});
