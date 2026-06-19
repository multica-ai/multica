// E2E test: API error with retry mechanism.
//
// Verifies that when the workflow GET API returns a 500 error, the page
// shows an error alert with a Retry button. After removing the interception
// and clicking Retry, the data loads successfully.
//
// Depends on: backend workflow API, frontend error + retry handling.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("API Error With Retry", () => {
  test("shows error alert on API 500 and retry loads data", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages via API ──
    const workflow = await seededApi.createWorkflow(
      "E2E API Retry Test " + Date.now()
    );
    await seededApi.createWorkflowStage(workflow.id, "Research", 1);
    await seededApi.createWorkflowStage(workflow.id, "Draft", 2);
    await seededApi.createWorkflowStage(workflow.id, "Review", 3);

    const overviewUrl = `/${slug}/workflows/${workflow.id}/overview`;

    // ── Step 1: Intercept the workflow GET API to return 500 ──
    // Intercept any request matching the workflow API endpoint
    await page.route(`**/api/workflows/${workflow.id}**`, (route) => {
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "Internal Server Error" }),
      });
    });

    // ── Step 2: Navigate to the overview page ──
    await page.goto(overviewUrl);
    await page.waitForLoadState("networkidle");

    // ── Step 3: Verify error alert/message is visible ──
    const errorAlert = page
      .getByRole("alert")
      .or(page.locator('[class*="error"]').first())
      .or(page.locator('[class*="destructive"]').first())
      .or(page.getByText(/error|failed|something went wrong|错误|失败|出错了/))
      .first();

    await expect(errorAlert).toBeVisible({ timeout: 5000 });

    // ── Step 4: Verify "Retry" button is present ──
    const retryButton = page
      .getByRole("button", { name: /retry|重试/i })
      .or(page.locator('button:has-text("Retry")'))
      .or(page.locator('button:has-text("重试")'))
      .first();

    await expect(retryButton).toBeVisible({ timeout: 3000 });

    // ── Step 5: Remove the API interception ──
    await page.unroute(`**/api/workflows/${workflow.id}**`);

    // ── Step 6: Click "Retry" ──
    await retryButton.click();

    // ── Step 7: Verify data loads successfully ──
    // After retry, stage cards should appear
    const stageCanvas = page
      .getByTestId("stage-canvas")
      .or(page.locator('[class*="stage-canvas"]'))
      .or(page.locator(".react-flow"))
      .first();

    await expect(stageCanvas).toBeVisible({ timeout: 10000 });

    // Verify stage cards exist
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    const cardCount = await stageCards.count();
    expect(cardCount).toBeGreaterThanOrEqual(1);

    // ── Step 8: Verify error alert is no longer visible ──
    await expect(errorAlert).not.toBeVisible({ timeout: 5000 });
  });
});
