import { expect, test, type Locator } from "@playwright/test";
import { loginAsDefault } from "./helpers";

/**
 * MUL-7296: the sort-direction toggle became a text label inside a row sized
 * for an icon, so "Newest first" rendered past the Display popover's edge and
 * squeezed the field select down to "Created c".
 */
test("keeps the Display popover's ordering controls inside the popover", async ({
  page,
}) => {
  await loginAsDefault(page);
  await page.getByRole("button", { name: "Display", exact: true }).click();
  const popover = page.locator("[data-slot=popover-content]");
  const field = popover.locator("[aria-label=Ordering]");
  await expect(field).toBeVisible();

  async function expectFits(direction: Locator) {
    const bounds = await popover.boundingBox();
    const fieldBox = await field.boundingBox();
    const directionBox = await direction.boundingBox();
    if (!bounds || !fieldBox || !directionBox) {
      throw new Error("Could not measure the ordering controls");
    }
    const right = bounds.x + bounds.width;
    expect(fieldBox.x + fieldBox.width).toBeLessThanOrEqual(right);
    expect(directionBox.x + directionBox.width).toBeLessThanOrEqual(right);
    const clipped = await field
      .locator("[data-slot=select-value]")
      .evaluate((el) => el.scrollWidth > el.clientWidth);
    expect(clipped).toBe(false);
  }

  // The reported default: Created date, newest first.
  await expectFits(popover.getByRole("button", { name: "Newest first", exact: true }));

  // The widest direction label.
  await field.click();
  await page.getByRole("option", { name: "Status", exact: true }).click();
  await popover.getByRole("button", { name: "Workflow order", exact: true }).click();
  await expectFits(
    popover.getByRole("button", { name: "Reverse workflow order", exact: true }),
  );
});
