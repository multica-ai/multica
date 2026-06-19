// E2E test: Node detail panel opens on DAG node click.
//
// Verifies that clicking a node in the DAG canvas causes a slide-out
// detail panel (drawer) to appear on the right side, showing the node
// name and the four key sections: Worker, Critic, Format Schema, Relations.
//
// Depends on: backend workflow + stage + node API, frontend overview page
// with DAG and detail panel implementation.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Node Detail Panel - Open", () => {
  test("clicking a node in the DAG opens the detail panel with all sections", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Detail Panel Open " + Date.now(),
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Processing",
      1,
    );

    // Create nodes in the stage with worker/critic config
    const nodeA = await seededApi.createWorkflowNode(workflow.id, {
      title: "Input Validation",
      stage_id: stage.id,
      position_x: 100,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    await seededApi.createWorkflowNode(workflow.id, {
      title: "Transform Data",
      stage_id: stage.id,
      position_x: 350,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`,
    );

    // ── Select the stage ──
    const stageCard = page
      .getByTestId(`stage-card-${stage.id}`)
      .or(page.locator(`[data-stage-id="${stage.id}"]`))
      .or(page.getByText("Processing").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area is visible with nodes ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Click a node in the DAG ──
    const dagNode = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node"),
    );

    await expect(dagNode.first()).toBeVisible({ timeout: 3000 });

    // Click the first node
    await dagNode.first().click();
    await page.waitForTimeout(500);

    // ── Step 2: Verify a slide-out detail panel appears ──
    const detailPanel = page
      .getByTestId("node-detail-panel")
      .or(page.locator('[role="dialog"]'))
      .or(page.locator('[role="complementary"]'))
      .or(page.locator('[class*="detail-panel"]'))
      .or(page.locator('[class*="node-detail"]'))
      .or(page.locator('[class*="side-panel"]'));

    await expect(detailPanel).toBeVisible({ timeout: 3000 });

    // ── Step 3: Verify the panel shows the node name as title ──
    // The panel should display the clicked node's title somewhere prominent
    await expect(
      detailPanel.locator("text=/Input Validation/"),
    ).toBeVisible({ timeout: 3000 });

    // ── Step 4: Verify all four key sections are present ──
    // Worker section
    const workerSection = detailPanel.locator("text=/Worker/");
    await expect(workerSection.first()).toBeVisible({ timeout: 3000 });

    // Critic section
    const criticSection = detailPanel.locator("text=/Critic/");
    await expect(criticSection.first()).toBeVisible({ timeout: 3000 });

    // Format Schema section
    const formatSchemaSection = detailPanel.locator("text=/Format Schema/");
    await expect(formatSchemaSection.first()).toBeVisible({ timeout: 3000 });

    // Relations section
    const relationsSection = detailPanel.locator("text=/Relations/");
    await expect(relationsSection.first()).toBeVisible({ timeout: 3000 });

    // ── Step 5: Verify DAG node gets selected visual state ──
    // After clicking, the clicked node should have a selected/active class
    const selectedNode = await dagNode.evaluateAll((nodes) => {
      return nodes.some(
        (n) =>
          n.classList.contains("selected") ||
          n.classList.contains("active") ||
          n.getAttribute("aria-selected") === "true" ||
          n.getAttribute("data-selected") === "true",
      );
    });
    expect(selectedNode).toBe(true);
  });
});
