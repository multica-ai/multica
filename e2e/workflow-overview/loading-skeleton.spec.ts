// E2E test: Loading skeleton displayed during API fetch.
//
// Intercepts the workflow GET API with a 2-second delay, navigates to
// the overview page, and verifies:
//   1. Skeleton placeholder cards appear during load
//   2. Skeleton disappears and real cards appear after the response
//
// Depends on: backend workflow + stage API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Loading Skeleton", () => {
  test("skeleton cards display during data fetch and are replaced by real cards", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages ──
    const workflow = await seededApi.createWorkflow(
      "E2E Skeleton Test " + Date.now()
    );

    await seededApi.createWorkflowStage(workflow.id, "Research", 1);
    await seededApi.createWorkflowStage(workflow.id, "Draft", 2);
    await seededApi.createWorkflowStage(workflow.id, "Review", 3);

    // ── Step 1: Intercept the workflow GET API with a delay ──
    // Route all workflow API requests and delay the response by 2 seconds
    await page.route("**/api/workflows/**", async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      await route.continue();
    });

    // ── Step 2: Navigate to the overview page ──
    const navigationPromise = page.goto(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 3: Wait briefly for skeleton to render ──
    // The skeleton should appear quickly since it's part of the initial render
    // before the API responds (with the 2s delay).
    await page.waitForTimeout(300);

    // Verify skeleton placeholder cards are visible
    const skeletonCards = page
      .getByTestId("stage-canvas-skeleton")
      .or(page.locator('[class*="skeleton"]'))
      .or(page.locator(".animate-pulse"));

    await expect(skeletonCards.first()).toBeVisible({ timeout: 2000 });

    // Verify at least 3 skeleton cards (matching the 3 stages we created)
    // If the skeleton is a single container, verify it contains pulsing elements
    const skeletonCount = await skeletonCards.count();
    // The skeleton may be a single container or individual card placeholders
    expect(skeletonCount).toBeGreaterThanOrEqual(1);

    // ── Step 4: Wait for the API response ──
    await navigationPromise;
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 5: Verify skeleton is gone and real cards appear ──
    // The skeleton should no longer be visible
    await expect(skeletonCards.first()).not.toBeVisible({ timeout: 3000 });

    // Real stage cards should appear
    const realCards = page
      .getByTestId(/^stage-card-/)
      .or(page.locator('[class*="stage-card"]'));

    await expect(realCards.first()).toBeVisible({ timeout: 3000 });

    // Verify we have the expected number of cards
    const cardCount = await realCards.count();
    expect(cardCount).toBe(3);

    // ── Step 6: Verify no error state is shown ──
    const errorElements = page
      .getByRole("alert")
      .or(page.locator('[class*="error"]'))
      .or(page.locator('[class*="destructive"]'));

    await expect(errorElements).not.toBeVisible();

    // Clean up the route interception
    await page.unroute("**/api/workflows/**");
  });
});
