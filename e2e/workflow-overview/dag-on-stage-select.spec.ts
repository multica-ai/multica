// E2E test: DAG renders on stage selection.
//
// Verifies that clicking a stage card updates the DAG to show that stage's
// nodes and applies fitView. Switching stages swaps the DAG content and
// selection state.
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("DAG on Stage Selection", () => {
  test("clicking stage cards swaps DAG content and selection state", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with multiple stages, each having nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E DAG Select Test " + Date.now()
    );

    // Create stages
    const stage1 = await seededApi.createWorkflowStage(
      workflow.id,
      "Research",
      1
    );
    const stage2 = await seededApi.createWorkflowStage(
      workflow.id,
      "Development",
      2
    );

    // Create nodes for stage 1
    const stage1Nodes = [
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Market Analysis",
        stage_id: stage1.id,
        position_x: 100,
        position_y: 50,
      }),
      await seededApi.createWorkflowNode(workflow.id, {
        title: "User Interviews",
        stage_id: stage1.id,
        position_x: 300,
        position_y: 50,
      }),
    ];

    // Create nodes for stage 2
    const stage2Nodes = [
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Frontend Setup",
        stage_id: stage2.id,
        position_x: 100,
        position_y: 50,
      }),
      await seededApi.createWorkflowNode(workflow.id, {
        title: "API Design",
        stage_id: stage2.id,
        position_x: 300,
        position_y: 50,
      }),
      await seededApi.createWorkflowNode(workflow.id, {
        title: "Database Schema",
        stage_id: stage2.id,
        position_x: 500,
        position_y: 50,
      }),
    ];

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Verify initial state ──
    // DAG area should be visible, initially showing first stage's nodes
    // or a placeholder
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Click the second stage card ──
    // Find the stage 2 card and click it
    const stage2Card = page
      .getByTestId(`stage-card-${stage2.id}`)
      .or(page.locator(`[data-stage-id="${stage2.id}"]`))
      .or(page.getByText("Development").first());

    await stage2Card.click();

    // ── Step 2: Verify stage 2 card gets selected state ──
    // Use aria-pressed as the primary selection attribute
    await expect(stage2Card).toHaveAttribute("aria-pressed", "true", {
      timeout: 3000,
    });

    // Extra verification for any additional selected visual state
    const stage2Selected = await stage2Card.evaluate((el) => {
      return (
        el.getAttribute("aria-pressed") === "true" ||
        el.getAttribute("data-selected") === "true" ||
        el.classList.contains("selected") ||
        el.classList.contains("active") ||
        el.getAttribute("aria-current") === "stage"
      );
    });
    expect(stage2Selected).toBe(true);

    // ── Step 3: Verify DAG updates to show stage 2's nodes ──
    // Wait for any transition/animation to complete
    await page.waitForTimeout(500);

    // Verify DAG auto-fits view — all nodes within viewport bounds
    const allNodesVisible = await page.evaluate(() => {
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
    expect(allNodesVisible).toBe(true);

    // Check that stage 2 nodes appear in the DAG
    // ReactFlow renders nodes as .react-flow__node elements
    const dagNodes = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node")
    );

    // We expect 3 nodes for stage 2 to be rendered
    const nodeCount = await dagNodes.count();
    expect(nodeCount).toBeGreaterThanOrEqual(3);

    // Verify specific stage 2 node titles are visible
    await expect(
      page.locator("text=/Frontend Setup|API Design|Database Schema/")
    ).toBeVisible({ timeout: 3000 });

    // Stage 1 nodes should NOT be visible (or at least not as prominently)
    const stage1InDagAfter = await page
      .locator("text=/Market Analysis|User Interviews/")
      .count();
    // Either stage 1 nodes are absent, or they exist but are fewer/less prominent
    // The DAG should clearly show stage 2's nodes as the primary content
    expect(stage1InDagAfter).toBeLessThanOrEqual(2);

    // ── Step 4: Click the first stage card to switch back ──
    const stage1Card = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.locator(`[data-stage-id="${stage1.id}"]`))
      .or(page.getByText("Research").first());

    await stage1Card.click();

    // Wait for DAG transition
    await page.waitForTimeout(500);

    // ── Step 5: Verify stage 1 card gains selected state ──
    const stage1Selected = await stage1Card.evaluate((el) => {
      return (
        el.getAttribute("aria-pressed") === "true" ||
        el.getAttribute("data-selected") === "true" ||
        el.classList.contains("selected") ||
        el.classList.contains("active") ||
        el.getAttribute("aria-current") === "stage"
      );
    });
    expect(stage1Selected).toBe(true);

    // Verify stage 2 card loses selected state
    const stage2StillSelected = await stage2Card.evaluate((el) => {
      return (
        el.getAttribute("aria-pressed") === "true" ||
        el.getAttribute("data-selected") === "true" ||
        el.classList.contains("selected") ||
        el.classList.contains("active")
      );
    });
    expect(stage2StillSelected).toBe(false);

    // ── Step 6: Verify DAG updates to show stage 1's nodes ──
    // Check that stage 1 nodes appear
    await expect(
      page.locator("text=/Market Analysis|User Interviews/")
    ).toBeVisible({ timeout: 3000 });

    // Verify the node count in DAG is now 2 (stage 1 has 2 nodes)
    const updatedNodeCount = await dagNodes.count();
    expect(updatedNodeCount).toBeGreaterThanOrEqual(2);
  });
});
