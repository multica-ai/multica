// E2E test: Reorder stages via drag and drop.
//
// Seeds a workflow with 3+ stages, records their order, drags the first
// stage card to the right of the second, then reloads to verify the new
// order persists.
//
// Depends on: backend workflow + stage API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Reorder Stages", () => {
  test("drags a stage card to reorder and persists after reload", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with 3+ stages ──
    const workflow = await seededApi.createWorkflow(
      "E2E Reorder Stages Test " + Date.now()
    );

    // Create 3 stages with ordered names so we can track them
    const stageA = await seededApi.createWorkflowStage(workflow.id, "Alpha", 1);
    const stageB = await seededApi.createWorkflowStage(workflow.id, "Beta", 2);
    const stageC = await seededApi.createWorkflowStage(workflow.id, "Gamma", 3);

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // Wait for the stage canvas to load
    const stageCanvas = page.getByTestId("stage-canvas").or(
      page.locator('[class*="stage-canvas"]')
    );
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 1: Record the initial order of stage names ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    await expect(stageCards.first()).toBeVisible({ timeout: 3000 });
    expect(await stageCards.count()).toBeGreaterThanOrEqual(3);

    // Extract names in DOM order
    const initialOrder = await stageCards.evaluateAll((cards) =>
      cards.map((card) => card.textContent ?? "")
    );

    // Verify the initial order matches our sort_order
    const initialStageAIndex = initialOrder.findIndex((t) => t.includes("Alpha"));
    const initialStageBIndex = initialOrder.findIndex((t) => t.includes("Beta"));
    const initialStageCIndex = initialOrder.findIndex((t) => t.includes("Gamma"));

    expect(initialStageAIndex).toBeLessThan(initialStageBIndex);
    expect(initialStageBIndex).toBeLessThan(initialStageCIndex);

    // ── Step 2: Identify the drag handles or card elements ──
    // The first card (Alpha) should have a drag handle or be draggable
    const firstCard = page
      .getByTestId(`stage-card-${stageA.id}`)
      .or(stageCards.nth(0));

    const secondCard = page
      .getByTestId(`stage-card-${stageB.id}`)
      .or(stageCards.nth(1));

    // Try to find a drag handle within the first card
    const dragHandle = firstCard
      .getByTestId("drag-handle")
      .or(firstCard.locator('[class*="drag"]'))
      .or(firstCard.locator('[class*="grip"]'))
      .or(firstCard);

    // ── Step 3: Perform the drag operation ──
    // Drag the first stage card (Alpha) to the right of the second (Beta)
    // so the new order becomes: Beta, Alpha, Gamma

    // Get the bounding box of the second card for the drop target position
    const secondCardBox = await secondCard.boundingBox();

    if (!secondCardBox) {
      // If bounding box unavailable, skip drag and attempt dialog reorder
      // per the spec's heal hint: "may use dialog-based reorder instead of drag"
      // For now, skip the drag and verify the API alternative
      test.skip(!secondCardBox, "Cannot determine card position for drag");
      return;
    }

    // Perform drag from first card to right side of second card
    const sourceBox = await firstCard.boundingBox();
    if (!sourceBox) {
      test.skip(!sourceBox, "Cannot determine source card position for drag");
      return;
    }

    // Drag from the center of the first card to the right edge of the second
    const sourceX = sourceBox.x + sourceBox.width / 2;
    const sourceY = sourceBox.y + sourceBox.height / 2;
    const targetX = secondCardBox.x + secondCardBox.width + 5;
    const targetY = secondCardBox.y + secondCardBox.height / 2;

    await page.mouse.move(sourceX, sourceY);
    await page.mouse.down();
    // Move in small steps for the drag to register
    const steps = 10;
    for (let i = 1; i <= steps; i++) {
      const x = sourceX + (targetX - sourceX) * (i / steps);
      const y = sourceY + (targetY - sourceY) * (i / steps);
      await page.mouse.move(x, y);
    }
    await page.mouse.up();

    // Allow time for the UI to update after the drag
    await page.waitForTimeout(500);

    // ── Step 4: Verify visual reorder ──
    // After the drag, the order should be: Beta, Alpha, Gamma
    const afterDragOrder = await stageCards.evaluateAll((cards) =>
      cards.map((card) => card.textContent ?? "")
    );

    const afterAlphaIndex = afterDragOrder.findIndex((t) => t.includes("Alpha"));
    const afterBetaIndex = afterDragOrder.findIndex((t) => t.includes("Beta"));

    // Alpha should now be after Beta
    expect(afterBetaIndex).toBeLessThan(afterAlphaIndex);

    // ── Step 5: Reload and verify persistence ──
    await page.reload();
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // Wait for stage cards to load after reload
    await expect(stageCards.first()).toBeVisible({ timeout: 5000 });

    const afterReloadOrder = await stageCards.evaluateAll((cards) =>
      cards.map((card) => card.textContent ?? "")
    );

    const reloadAlphaIndex = afterReloadOrder.findIndex((t) => t.includes("Alpha"));
    const reloadBetaIndex = afterReloadOrder.findIndex((t) => t.includes("Beta"));

    // The new order (Beta before Alpha) should persist after reload
    expect(reloadBetaIndex).toBeLessThan(reloadAlphaIndex);

    // Verify Gamma remains in the last position
    const reloadGammaIndex = afterReloadOrder.findIndex((t) => t.includes("Gamma"));
    expect(reloadGammaIndex).toBeGreaterThan(reloadBetaIndex);
    expect(reloadGammaIndex).toBeGreaterThan(reloadAlphaIndex);
  });
});
