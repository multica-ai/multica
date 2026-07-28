import { expect, test } from "@playwright/test";

import type { TestApiClient } from "./fixtures";
import { createTestApi, loginAsDefault } from "./helpers";

test.describe("FIR-3545 authenticated iOS Shortcut share", () => {
  test.setTimeout(120_000);

  let api: TestApiClient;
  let createdIssueId: string | undefined;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (createdIssueId) {
      await api.deleteIssue(createdIssueId);
    }
    await api.cleanup();
  });

  test("creates in the fixed project through the signed-in page", async ({
    page,
  }) => {
    const workspace = await api.ensureWorkspace();
    const project = await api.createProject(`Shortcut target ${Date.now()}`);
    const sharedText = `Shortcut share ${Date.now()}`;
    const hash = new URLSearchParams({
      shortcut: "1",
      workspace_id: workspace.id,
      project_id: project.id,
      text: sharedText,
      submit: "1",
    });

    await page.goto(`/share/issue#${hash.toString()}`, {
      waitUntil: "domcontentloaded",
    });
    await page.waitForURL(/\/issues\/[0-9a-f-]{36}$/i, { timeout: 15_000 });

    createdIssueId = new URL(page.url()).pathname.split("/").at(-1);
    await expect(
      page.getByRole("heading", { name: sharedText }).first(),
    ).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText(project.title, { exact: true }).first()).toBeVisible();
  });
});
