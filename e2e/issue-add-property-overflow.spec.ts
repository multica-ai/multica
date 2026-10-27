import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";
import type { TestApiClient } from "./fixtures";

// The server caps a workspace at 20 active properties; together with the
// built-in optional fields that is far taller than a 600px window.
const PROPERTY_COUNT = 20;

/**
 * MUL-8280: the issue sidebar's "+ Add property" list is a Popover, which has
 * no height bound of its own. With a long workspace property catalog the list
 * ran past the bottom of the window and its tail options could not be reached.
 */
test.describe("Issue sidebar add property", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("keeps a long property list inside the viewport and lets the last option be picked", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 600 });
    const suffix = Date.now().toString(36);
    const names: string[] = [];
    for (let i = 0; i < PROPERTY_COUNT; i++) {
      const name = `Overflow ${suffix} ${String(i).padStart(2, "0")}`;
      await api.createProperty(name);
      names.push(name);
    }
    const title = `E2E add property overflow ${suffix}`;
    const issue = await api.createIssue(title);
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, title);

    await page.locator("[data-right-sidebar-panel]").getByRole("button", { name: "Add property" }).click();
    const popover = page.getByTestId("add-property-popover");
    await expect(popover).toBeVisible();

    const viewport = page.viewportSize();
    const box = await popover.boundingBox();
    if (!viewport || !box) throw new Error("Could not measure the add property list");
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);

    // Wheel inside the list, then click the last option with the mouse: an
    // option outside the window is exactly what the report was about.
    const last = popover.getByRole("button", { name: names[names.length - 1] });
    await popover.hover();
    await page.mouse.wheel(0, 5000);
    // The list scrolls inside its own bounded box; wheel scrolling is async.
    await expect.poll(() => popover.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    await expect.poll(async () => {
      const b = await last.boundingBox();
      return b ? b.y + b.height : Infinity;
    }).toBeLessThanOrEqual(viewport.height);
    const lastBox = await last.boundingBox();
    if (!lastBox) throw new Error("Could not measure the last option");
    await page.mouse.click(lastBox.x + lastBox.width / 2, lastBox.y + lastBox.height / 2);

    await expect(popover).not.toBeVisible();
    await expect(page.getByText(names[names.length - 1], { exact: true })).toBeVisible();
  });
});
