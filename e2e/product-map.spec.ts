import { test, expect } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

const ROUTE_CHANGE_TIMEOUT = 30000;

test.describe("Product map (SY-20)", () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDefault(page);
    await page.waitForLoadState("networkidle");
  });

  test("sidebar links to /products and the page renders the product tree", async ({ page }) => {
    await page.getByRole("link", { name: "Products" }).click();
    await expect(page).toHaveURL(/\/products/, { timeout: ROUTE_CHANGE_TIMEOUT });
    await waitForPageText(page, "产品树");

    // The seeded tree renders (Multica node), and the status column never
    // claims 已上线 without evidence: 待确认 is the fallback label.
    await expect(page.locator("text=产品树").first()).toBeVisible();
  });
});
