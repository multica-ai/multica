import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Data sync settings", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can export workspace manifest from data tab", async ({ page }) => {
    const title = `E2E Export Seed ${Date.now()}`;
    await api.createIssue(title);

    await page.goto("/settings");
    await page.getByRole("tab", { name: "Data" }).click();

    const exportResponsePromise = page.waitForResponse((response) =>
      response.url().includes("/api/data/export") && response.request().method() === "GET",
    );
    await page.getByRole("button", { name: "Export JSON" }).click();
    const exportResponse = await exportResponsePromise;

    expect(exportResponse.ok()).toBeTruthy();
    const manifest = await exportResponse.json() as {
      schema_version: string;
      data: { issues: Array<{ title: string }> };
    };
    expect(manifest.schema_version).toBe("2026-05-31");
    expect(manifest.data.issues.some((issue) => issue.title === title)).toBeTruthy();
  });

  test("can dry-run and apply canonical import from data tab", async ({ page }) => {
    const importedTitle = `E2E Imported ${Date.now()}`;
    const workspaceId = await page.evaluate(() => localStorage.getItem("multica_workspace_id"));
    if (!workspaceId) {
      throw new Error("Missing workspace id in localStorage");
    }

    const payload = {
      schema_version: "2026-05-31",
      workspace: { id: workspaceId },
      data: {
        issues: [{ title: importedTitle, status: "backlog", priority: "none" }],
      },
    };

    await page.goto("/settings");
    await page.getByRole("tab", { name: "Data" }).click();
    await page.getByLabel("Manifest JSON").fill(JSON.stringify(payload));

    const dryRunResponsePromise = page.waitForResponse((response) =>
      response.url().includes("/api/data/import/dry-run") && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Dry Run" }).click();
    const dryRunResponse = await dryRunResponsePromise;
    expect(dryRunResponse.ok()).toBeTruthy();
    await expect(page.getByText("Dry-run completed")).toBeVisible({ timeout: 10000 });

    const applyResponsePromise = page.waitForResponse((response) =>
      response.url().includes("/api/data/import/apply") && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Apply Import" }).click();
    const applyResponse = await applyResponsePromise;
    expect(applyResponse.ok()).toBeTruthy();

    await page.goto("/issues");
    await expect(page.getByText(importedTitle).first()).toBeVisible({ timeout: 10000 });
  });
});
