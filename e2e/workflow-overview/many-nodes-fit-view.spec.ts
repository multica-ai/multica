// E2E test: Many nodes fitView and zoom/pan controls.
//
// Verifies that a stage with 15+ nodes renders all nodes within the viewport
// (fitView applied), zoom/pan controls are functional, zooming in enables
// scroll/pan, and clicking the fit-view button recenters the view.
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Many Nodes FitView", () => {
  test("stage with 15+ nodes all visible via fitView, zoom/pan controls work", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and 15+ nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Many Nodes Test " + Date.now()
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Large Stage",
      1
    );

    // Create 15+ nodes arranged in a grid pattern to test fitView
    const nodeNames: string[] = [];
    for (let row = 0; row < 4; row++) {
      for (let col = 0; col < 4; col++) {
        const name = `Node R${row + 1}C${col + 1}`;
        nodeNames.push(name);
        await seededApi.createWorkflowNode(workflow.id, {
          title: name,
          stage_id: stage.id,
          position_x: col * 250,
          position_y: row * 150,
        });
      }
    }
    // Total: 16 nodes (4x4 grid)

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Select the stage ──
    const stageCard = page
      .getByTestId(`stage-card-${stage.id}`)
      .or(page.locator(`[data-stage-id="${stage.id}"]`))
      .or(page.getByText("Large Stage").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area and ReactFlow controls ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Verify all nodes are visible within the viewport (fitView) ──
    const reactFlowNodes = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node")
    );

    // Wait for ReactFlow to render all nodes
    await expect(reactFlowNodes.first()).toBeVisible({ timeout: 3000 });
    const nodeCount = await reactFlowNodes.count();
    expect(nodeCount).toBeGreaterThanOrEqual(15);

    // Check that all node titles are present in the DOM
    // Pick a few representative nodes across the grid to verify visibility
    const sampleNodes = ["Node R1C1", "Node R4C4", "Node R2C3", "Node R3C1"];
    for (const nodeName of sampleNodes) {
      await expect(
        page.locator(`text=/${nodeName}/`)
      ).toBeVisible({ timeout: 2000 });
    }

    // Verify all nodes are within the viewport bounds (fitView applied)
    const allNodesVisible = await page.evaluate(() => {
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const nodes = document.querySelectorAll(".react-flow__node");
      if (nodes.length === 0) return false;

      let allInView = true;
      nodes.forEach((node) => {
        const rect = node.getBoundingClientRect();
        // Check if node center is within viewport (accounting for some padding)
        const nodeCX = rect.left + rect.width / 2;
        const nodeCY = rect.top + rect.height / 2;
        if (
          nodeCX < -50 ||
          nodeCX > viewportWidth + 50 ||
          nodeCY < -50 ||
          nodeCY > viewportHeight + 50
        ) {
          allInView = false;
        }
      });
      return allInView;
    });
    expect(allNodesVisible).toBe(true);

    // ── Step 2: Verify zoom/pan controls are functional ──
    // ReactFlow controls include zoom in, zoom out, and fit view buttons
    const zoomInButton = dagArea.locator(
      ".react-flow__controls-zoomin"
    ).or(page.locator(".react-flow__controls-zoomin"));

    const zoomOutButton = dagArea.locator(
      ".react-flow__controls-zoomout"
    ).or(page.locator(".react-flow__controls-zoomout"));

    const fitViewButton = dagArea.locator(
      ".react-flow__controls-fitview"
    ).or(page.locator(".react-flow__controls-fitview"));

    await expect(zoomInButton).toBeVisible({ timeout: 3000 });
    await expect(zoomOutButton).toBeVisible({ timeout: 2000 });
    await expect(fitViewButton).toBeVisible({ timeout: 2000 });

    // ── Step 3: Zoom in via controls and verify zoom effect ──
    // Get the current viewport transform before zoom
    const transformBefore = await page.evaluate(() => {
      const viewport = document.querySelector(".react-flow__viewport");
      if (!viewport) return null;
      return viewport.getAttribute("transform") || viewport.getAttribute("style");
    });

    // Click zoom in button
    await zoomInButton.click();
    await page.waitForTimeout(400);

    // Verify the transform changed (viewport zoomed in)
    const transformAfter = await page.evaluate(() => {
      const viewport = document.querySelector(".react-flow__viewport");
      if (!viewport) return null;
      return viewport.getAttribute("transform") || viewport.getAttribute("style");
    });

    // The transform should have changed after zoom
    expect(transformAfter).not.toBe(transformBefore);

    // After zooming in, some nodes may be outside the viewport
    // The dag area should still be visible and interactive
    await expect(dagArea).toBeVisible();

    // ── Step 4: Attempt pan after zoom ──
    // Verify the DAG pane is still interactive (pan-able)
    const dagPane = dagArea.locator(".react-flow__pane").or(
      page.locator(".react-flow__pane")
    );
    await expect(dagPane).toBeVisible();

    // Perform a pan gesture on the pane
    const paneBox = await dagPane.boundingBox();
    if (paneBox) {
      const panStartX = paneBox.x + paneBox.width / 2;
      const panStartY = paneBox.y + paneBox.height / 2;

      await page.mouse.move(panStartX, panStartY);
      await page.mouse.down();
      await page.mouse.move(panStartX - 100, panStartY - 50, { steps: 10 });
      await page.mouse.up();
      await page.waitForTimeout(300);
    }

    // ═─ Step 5: Click fit-view button to recenter ──
    await fitViewButton.click();
    await page.waitForTimeout(500);

    // Verify all nodes are back in view after fitView
    const allNodesVisibleAfterFit = await page.evaluate(() => {
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const nodes = document.querySelectorAll(".react-flow__node");
      if (nodes.length === 0) return false;

      let allInView = true;
      nodes.forEach((node) => {
        const rect = node.getBoundingClientRect();
        const nodeCX = rect.left + rect.width / 2;
        const nodeCY = rect.top + rect.height / 2;
        if (
          nodeCX < -50 ||
          nodeCX > viewportWidth + 50 ||
          nodeCY < -50 ||
          nodeCY > viewportHeight + 50
        ) {
          allInView = false;
        }
      });
      return allInView;
    });
    expect(allNodesVisibleAfterFit).toBe(true);

    // Verify the control buttons are still responsive
    await expect(zoomOutButton).toBeVisible();
    await expect(zoomInButton).toBeVisible();
  });
});
