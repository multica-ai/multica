import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, openWorkspaceMenu } from "./helpers";

test.describe("Authentication", () => {
  test("root path bootstraps directly into issues", async ({ page }) => {
    const api = await createTestApi();
    const workspace = await api.ensureWorkspace(
      "E2E Workspace",
      "e2e-workspace",
    );

    await page.goto("/");
    await page.waitForURL(`**/${workspace.slug}/issues`, { timeout: 10000 });
    await expect(page.locator("text=All Issues")).toBeVisible();
  });

  test("login route acts as a compatibility shell and redirects to issues", async ({
    page,
  }) => {
    const api = await createTestApi();
    const workspace = await api.ensureWorkspace(
      "E2E Workspace",
      "e2e-workspace",
    );

    await page.goto("/login");
    await page.waitForURL(`**/${workspace.slug}/issues`, { timeout: 10000 });
    await expect(page.locator("text=All Issues")).toBeVisible();
  });

  test("bootstrap enters the workspace without a manual login step", async ({
    page,
  }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/issues/);
    await expect(page.locator("text=All Issues")).toBeVisible();
  });

  test("workspace route bootstraps without redirecting to a manual login flow", async ({
    page,
  }) => {
    const api = await createTestApi();
    const workspace = await api.ensureWorkspace(
      "E2E Workspace",
      "e2e-workspace",
    );

    await page.goto(`/${workspace.slug}/issues`);
    await page.waitForURL(`**/${workspace.slug}/issues`, { timeout: 10000 });
    await expect(page).not.toHaveURL(/\/login/);
    await expect(page.locator("text=All Issues")).toBeVisible();
  });

  test("logout re-enters the app through bootstrap without staying on login", async ({
    page,
  }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    // Click Sign out
    await page.locator("text=Sign out").click();

    await page.waitForURL("**/issues", { timeout: 10000 });
    await expect(page).not.toHaveURL(/\/login$/);
    await expect(page.locator("text=All Issues")).toBeVisible();
  });
});
