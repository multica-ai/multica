import { test, expect } from "@playwright/test";
import { loginAsDefault } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.goto("/login");

    await expect(page.getByText("Multica")).toBeVisible();
    await expect(page.getByLabel("Email")).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue" })).toBeVisible();
  });

  test("login and redirect to /issues", async ({ page }) => {
    await loginAsDefault(page, test.info().parallelIndex);

    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByRole("link", { name: "Issues", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Board", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Notifications", exact: true })).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.removeItem("multica_token");
      localStorage.removeItem("multica_workspace_id");
    });

    await page.goto("/issues");
    await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10000 });
  });

  test("logout redirects to /login", async ({ page }) => {
    await loginAsDefault(page, test.info().parallelIndex);

    await page.getByRole("button", { name: "Log out" }).click();

    await page.waitForURL(/\/login(?:\?|$)/, { timeout: 10000 });
    await expect(page).toHaveURL(/\/login/);
  });
});
