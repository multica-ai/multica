import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, openWorkspaceMenu, waitForPageText } from "./helpers";

test.describe("Authentication", () => {
  test("login page renders correctly", async ({ page }) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Sign in to Multica");

    await expect(page.getByText("Sign in to Multica")).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Email" })).toBeVisible();
    await expect(page.getByPlaceholder("you@example.com")).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue" })).toBeDisabled();
  });

  test("login and redirect to /issues", async ({ page }) => {
    const workspaceSlug = await loginAsDefault(page);

    await expect(page).toHaveURL(new RegExp(`/${workspaceSlug}/issues$`));
    await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page }) => {
    const api = await createTestApi();
    const [workspace] = await api.getWorkspaces();
    if (!workspace) {
      throw new Error("E2E workspace was not created");
    }

    await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
    await page.waitForURL("**/login", { timeout: 10000, waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Sign in to Multica");
  });

  test("logout redirects to /login", async ({ page }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    await page.getByRole("menuitem", { name: "Log out" }).click();

    await page.waitForURL("**/login", { timeout: 10000, waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Sign in to Multica");
    await expect(page).toHaveURL(/\/login/);
  });
});
