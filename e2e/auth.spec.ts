import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.goto("/login");

    await expect(page.getByText("Sign in to Multica")).toBeVisible();
    await expect(page.getByLabel("Email")).toBeVisible();
    await expect(page.getByPlaceholder("you@example.com")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Continue" }),
    ).toBeDisabled();

    await page.getByLabel("Email").fill("e2e-login-page@multica.ai");
    await expect(
      page.getByRole("button", { name: "Continue" }),
    ).toBeEnabled();
  });

  test("email login advances to code step", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(`e2e-ui-${Date.now()}@multica.ai`);
    await page.getByRole("button", { name: "Continue" }).click();

    await expect(page.getByText("Check your email")).toBeVisible();
    await expect(page.getByText("Resend in")).toBeVisible();
  });

  test("login and redirect to /issues", async ({ page }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByText("Issues").first()).toBeVisible();
  });

  test("unauthenticated user is redirected to /", async ({ page }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.removeItem("multica_token");
      localStorage.removeItem("multica_workspace_id");
    });

    await page.goto("/issues");
    await page.waitForURL(/\/$/, { timeout: 10000 });
    await expect(page).toHaveURL(/\/$/);
  });

  test("logout redirects to /", async ({ page }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    // Click Log out
    await page.getByRole("menuitem", { name: "Log out" }).click();

    await page.waitForURL(/\/$/, { timeout: 10000 });
    await expect(page).toHaveURL(/\/$/);
  });
});
