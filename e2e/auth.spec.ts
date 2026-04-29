import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.context().clearCookies();
    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.removeItem("multica_token");
    });
    await page.reload();

    await expect(
      page.getByText("Sign in to Multica"),
    ).toBeVisible();
    await expect(page.getByLabel("Email")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Continue" }),
    ).toBeVisible();
  });

  test("login and redirect to /issues", async ({ page }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page }) => {
    await page.goto("/login");
    await page.evaluate(() => {
      localStorage.removeItem("multica_token");
    });

    // Visit a workspace-scoped route; DashboardGuard should redirect to /login.
    // The slug here need not exist — the guard runs before workspace resolution
    // for unauthenticated users.
    await page.goto("/e2e-workspace/issues");
    await page.waitForURL("**/login", { timeout: 10000 });
  });

  test("logout redirects to /login", async ({ page }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    await page.getByText("Log out").click();

    await page.waitForURL("**/login", { timeout: 10000 });
    await expect(page).toHaveURL(/\/login/);
  });
});
