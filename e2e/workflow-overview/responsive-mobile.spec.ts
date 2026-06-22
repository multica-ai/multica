// E2E test: Responsive layout below breakpoint (mobile).
//
// Verifies the overview page layout at 800x600 viewport (below 1024px
// breakpoint):
//   1. Stage cards stack vertically (accordion or list layout)
//   2. No horizontal scroll on the stage strip
//   3. Clicking a stage renders DAG below/within the expanded section
//   4. Detail panel opens as a bottom drawer (full-screen on mobile)
//
// Depends on: backend workflow + stage + node API, frontend responsive layout.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Responsive Mobile Layout", () => {
  test("overview page adapts to mobile viewport with vertical layout and bottom drawer", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Responsive Test " + Date.now()
    );

    const stage1 = await seededApi.createWorkflowStage(
      workflow.id,
      "Research",
      1
    );
    const stage2 = await seededApi.createWorkflowStage(
      workflow.id,
      "Design",
      2
    );
    const stage3 = await seededApi.createWorkflowStage(
      workflow.id,
      "Review",
      3
    );

    // Add a node so we can test detail panel
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Mobile Node",
      stage_id: stage1.id,
    });

    // ── Step 1: Resize the viewport to 800x600 (below 1024px breakpoint) ──
    await page.setViewportSize({ width: 800, height: 600 });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // Wait for the page to load
    const pageContainer = page
      .getByTestId("overview-page")
      .or(page.locator('[class*="overview"]').first())
      .or(page.locator("main"))
      .first();

    await expect(pageContainer).toBeVisible({ timeout: 5000 });

    // ── Step 2: Verify stage cards are stacked vertically ──
    // On mobile, stage cards should have a vertical layout (same x, different y)
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    // Wait for at least some cards to appear
    const cardCount = await stageCards.count();
    expect(cardCount).toBeGreaterThanOrEqual(3);

    // Verify vertical stacking: cards should have roughly the same x position
    // but different y positions (vertical layout)
    const cardPositions = await stageCards.evaluateAll((cards) =>
      cards.map((card) => {
        const rect = card.getBoundingClientRect();
        return { x: rect.x, y: rect.y };
      })
    );

    // All cards should have similar x values (within ~10px of each other)
    const xValues = cardPositions.map((pos) => Math.round(pos.x));
    const xRange = Math.max(...xValues) - Math.min(...xValues);
    expect(xRange).toBeLessThan(50); // Cards should be vertically aligned

    // Cards should have distinct y values (stacked vertically)
    const yValues = cardPositions.map((pos) => Math.round(pos.y));
    const uniqueYValues = new Set(yValues);
    expect(uniqueYValues.size).toBeGreaterThanOrEqual(2); // At least 2 vertical positions

    // ── Step 3: Verify no horizontal scroll on stage strip ──
    const stageStrip = page
      .getByTestId("stage-card-strip")
      .or(page.locator('[class*="stage-strip"]'))
      .or(pageContainer);

    const hasHorizontalScroll = await stageStrip.evaluate((el) => {
      return el.scrollWidth > el.clientWidth;
    });

    // On mobile (vertical layout), the stage strip should not overflow
    // horizontally
    expect(hasHorizontalScroll).toBe(false);

    // ── Step 4: Click a stage and verify DAG renders below/within ──
    const firstCard = stageCards.first();
    await firstCard.click();

    // The DAG area should be visible (either below the expanded section
    // or within an accordion)
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="stage-dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 3000 });

    // On mobile, the DAG should be within the viewport (vertical layout)
    const dagRect = await dagArea.boundingBox();
    expect(dagRect).not.toBeNull();
    if (dagRect) {
      // DAG should be below the stage cards (larger y)
      const cardY = Math.min(...yValues);
      expect(dagRect.y).toBeGreaterThanOrEqual(cardY);
    }

    // ── Step 5: Open detail panel — verify bottom drawer ──
    // Click on a node in the DAG to open the detail panel
    const nodeElement = dagArea
      .locator(".react-flow__node")
      .or(page.locator('[class*="workflow-node"]'))
      .first();

    await expect(nodeElement).toBeVisible({ timeout: 3000 });
    await nodeElement.click();

    // Verify the detail panel opens as a bottom drawer
    // On mobile, the drawer should be at the bottom of the viewport
    // (full-width, positioned at bottom)
    const detailPanel = page
      .getByTestId("detail-panel")
      .or(page.locator('[role="dialog"]'))
      .or(page.locator('[class*="drawer"]'))
      .or(page.locator('[class*="bottom-sheet"]'))
      .or(page.locator('[class*="detail-panel"]'))
      .first();

    await expect(detailPanel).toBeVisible({ timeout: 3000 });

    // Verify the drawer is at the bottom of the viewport
    // (or takes full screen on mobile)
    const panelRect = await detailPanel.boundingBox();
    expect(panelRect).not.toBeNull();
    if (panelRect) {
      // On mobile, the panel should be wide (full-width or nearly full-width)
      const viewportWidth = 800;
      expect(panelRect.width).toBeGreaterThan(viewportWidth * 0.7);

      // The panel should be at or near the bottom of the viewport
      // (or full-screen)
      const viewportHeight = 600;
      const nearBottom = panelRect.y + panelRect.height >= viewportHeight - 10;
      const isFullScreen =
        panelRect.y <= 50 && panelRect.height >= viewportHeight - 50;
      expect(nearBottom || isFullScreen).toBe(true);
    }
  });
});
