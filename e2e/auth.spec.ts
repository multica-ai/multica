import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, openAccountMenu } from "./helpers";

async function expectIssuesBoard(page: Parameters<typeof test>[0]["page"]) {
  await expect(page.getByText("Backlog")).toBeVisible();
  await expect(page.getByRole("button", { name: "All" })).toBeVisible();
}

test.describe("Authentication", () => {
  test("root path bootstraps directly into issues", async ({ page }) => {
    const api = await createTestApi();
    const workspace = await api.ensureWorkspace(
      "E2E Workspace",
      "e2e-workspace",
    );

    await page.goto("/");
    await page.waitForURL(`**/${workspace.slug}/issues`, { timeout: 10000 });
    await expectIssuesBoard(page);
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
    await expectIssuesBoard(page);
  });

  test("bootstrap enters the workspace without a manual login step", async ({
    page,
  }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/issues/);
    await expectIssuesBoard(page);
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
    await expectIssuesBoard(page);
  });

  test("logout re-enters the app through bootstrap without staying on login", async ({
    page,
  }) => {
    await loginAsDefault(page);

    await openAccountMenu(page);

    await page.getByText("Log out").click();

    await page.waitForURL("**/issues", { timeout: 10000 });
    await expect(page).not.toHaveURL(/\/login$/);
    await expectIssuesBoard(page);
  });
});
