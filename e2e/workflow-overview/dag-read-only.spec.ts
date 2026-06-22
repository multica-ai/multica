// E2E test: DAG is read-only in overview page.
//
// Verifies that the DAG canvas renders in a read-only mode:
//   - Nodes cannot be dragged (attempted drag leaves them in place)
//   - No edge creation handles are visible
//   - Delete key does nothing to selected nodes
//   - Clicking a node opens the detail panel, not edit mode
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("DAG Read-Only Mode", () => {
  test("DAG is read-only: no drag, no edge handles, no delete, click opens detail panel", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E DAG ReadOnly Test " + Date.now()
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Processing",
      1
    );

    // Create several nodes in the stage
    const nodes = [
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Input Validation",
        stage_id: stage.id,
        position_x: 100,
        position_y: 50,
      }),
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Transform Data",
        stage_id: stage.id,
        position_x: 350,
        position_y: 50,
      }),
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Generate Output",
        stage_id: stage.id,
        position_x: 600,
        position_y: 50,
      }),
    ];

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Select the stage (click its card) ──
    const stageCard = page
      .getByTestId(`stage-card-${stage.id}`)
      .or(page.locator(`[data-stage-id="${stage.id}"]`))
      .or(page.getByText("Processing").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area is visible ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Verify nodes exist in the DAG ──
    const dagNodes = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node")
    );

    const initialNodeCount = await dagNodes.count();
    expect(initialNodeCount).toBeGreaterThanOrEqual(3);

    // ── Step 2: Verify nodes cannot be dragged ──
    // Attempt to drag the first node
    const firstNode = dagNodes.first();
    const firstNodeBox = await firstNode.boundingBox();

    if (firstNodeBox) {
      // Perform a drag operation on the node
      const startX = firstNodeBox.x + firstNodeBox.width / 2;
      const startY = firstNodeBox.y + firstNodeBox.height / 2;

      await page.mouse.move(startX, startY);
      await page.mouse.down();
      await page.mouse.move(startX + 200, startY + 100, { steps: 10 });
      await page.mouse.up();

      // After drag attempt, verify the node hasn't moved (read-only)
      const updatedBox = await firstNode.boundingBox();
      if (updatedBox) {
        // Position should be approximately the same (allow minor rounding)
        const dx = Math.abs(updatedBox.x - firstNodeBox.x);
        const dy = Math.abs(updatedBox.y - firstNodeBox.y);
        expect(dx).toBeLessThan(5);
        expect(dy).toBeLessThan(5);
      }
    }

    // ── Step 3: Verify no node is selected after drag attempt ──
    // In read-only mode, clicking/dragging should not select nodes
    const selectedNodeExists = await page.evaluate(() => {
      return document.querySelector(".react-flow__node.selected") !== null;
    });
    expect(selectedNodeExists).toBe(false);

    // ── Step 4: Verify no edge creation handles ──
    // ReactFlow edge creation handles have class .react-flow__handle
    // In read-only mode with nodesDraggable=false, handles should not be
    // interactive or visible as connection points
    const edgeHandles = page.locator(".react-flow__handle-connection").or(
      page.locator('[class*="handle"][class*="connect"]')
    );
    const handleCount = await edgeHandles.count();
    expect(handleCount).toBe(0);

    // Also verify that standard handles don't have connection-enabled classes
    const hasConnectionHandle = await page.evaluate(() => {
      const handles = document.querySelectorAll(".react-flow__handle");
      let connectable = false;
      handles.forEach((h) => {
        if (
          h.classList.contains("connectable") ||
          h.getAttribute("data-handlepos") !== null
        ) {
          // Check if any handle has a connection ability indicator
          const style = window.getComputedStyle(h);
          if (style.cursor === "crosshair" || style.cursor === "grab") {
            connectable = true;
          }
        }
      });
      return connectable;
    });
    expect(hasConnectionHandle).toBe(false);

    // ── Step 5: Verify Delete key does nothing ──
    // Click on a node first (to potentially select it)
    if (firstNodeBox) {
      await firstNode.click();
      await page.waitForTimeout(200);
    }

    // Press Delete key
    await page.keyboard.press("Delete");
    await page.waitForTimeout(300);

    // Verify nodes are still present after Delete key
    const nodeCountAfterDelete = await dagNodes.count();
    expect(nodeCountAfterDelete).toBe(initialNodeCount);

    // ── Step 6: Verify clicking a node opens the detail panel (not edit mode) ──
    // Click on a node
    if (firstNodeBox) {
      await firstNode.click();
      await page.waitForTimeout(300);
    }

    // A detail panel should appear (not an inline editor)
    const detailPanel = page
      .getByTestId("detail-panel")
      .or(page.locator('[class*="detail-panel"]'))
      .or(page.locator('[class*="node-detail"]'))
      .or(page.locator('[class*="side-panel"]'));

    await expect(detailPanel).toBeVisible({ timeout: 3000 });

    // Verify no inline edit mode is active (no input fields inside the node)
    const nodeHasInput = await firstNode.evaluate((el) => {
      return el.querySelector("input, textarea, [contenteditable]") !== null;
    });
    expect(nodeHasInput).toBe(false);

    // Verify the detail panel shows node information
    await expect(
      detailPanel.or(page.locator("text=/Input Validation|Transform Data|Generate Output/"))
    ).toBeVisible({ timeout: 3000 });
  });
});
