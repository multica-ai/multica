import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Plan", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }, testInfo) => {
    api = await createTestApi(testInfo.parallelIndex);
    await loginAsDefault(page, testInfo.parallelIndex);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("can create edit and add issue candidates from the plan page", async ({ page }) => {
    const manualTitle = `E2E Manual Plan ${Date.now()}`;
    const candidateTitle = `E2E Candidate Plan ${Date.now()}`;
    await api.createIssue(candidateTitle, { priority: "high" });

    await page.goto("/plan");
    await expect(page.getByRole("heading", { name: "Plan" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Candidates" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Capacity" })).toBeVisible();

    await page.getByPlaceholder("Add plan item").fill(manualTitle);
    await page.getByPlaceholder("Min", { exact: true }).fill("30");
    await page.getByRole("button", { name: "Add" }).first().click();
    await expect(page.getByText(manualTitle)).toBeVisible();

    await page.getByRole("button", { name: "Edit plan item" }).first().click();
    await expect(page.getByRole("heading", { name: "Plan item" })).toBeVisible();
    await page.getByLabel("Note").fill("E2E plan item note");
    await page.getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByRole("heading", { name: "Plan item" })).toBeHidden();
    await expect(page.getByText("E2E plan item note")).toBeVisible();

    await expect(page.getByText(candidateTitle)).toBeVisible();
    const candidateRow = page.getByText(candidateTitle).locator("xpath=ancestor::div[contains(@class, 'rounded-lg')][1]");
    await candidateRow.getByRole("button", { name: "Add" }).click();
    await expect(page.locator("section").getByText(candidateTitle)).toBeVisible();
  });
});
