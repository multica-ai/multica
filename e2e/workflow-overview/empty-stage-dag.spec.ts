// E2E test: Empty stage DAG placeholder.
//
// Verifies that a stage with zero nodes shows an appropriate empty state
// message in the DAG area, and that switching to a non-empty stage
// restores the normal DAG view.
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Empty Stage DAG Placeholder", () => {
  test("empty stage shows placeholder message, non-empty stage shows nodes", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with one empty stage and one non-empty stage ──
    const workflow = await seededApi.createWorkflow(
      "E2E Empty Stage DAG Test " + Date.now()
    );

    // Create an empty stage (no nodes assigned)
    const emptyStage = await seededApi.createWorkflowStage(
      workflow.id,
      "Empty Stage",
      1
    );

    // Create a non-empty stage with nodes
    const nonEmptyStage = await seededApi.createWorkflowStage(
      workflow.id,
      "Active Stage",
      2
    );

    await seededApi.createWorkflowNode(workflow.id, {
      title: "Task Alpha",
      stage_id: nonEmptyStage.id,
      position_x: 100,
      position_y: 50,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Task Beta",
      stage_id: nonEmptyStage.id,
      position_x: 350,
      position_y: 50,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Task Gamma",
      stage_id: nonEmptyStage.id,
      position_x: 600,
      position_y: 50,
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 1: Select the empty stage ──
    const emptyStageCard = page
      .getByTestId(`stage-card-${emptyStage.id}`)
      .or(page.locator(`[data-stage-id="${emptyStage.id}"]`))
      .or(page.getByText("Empty Stage").first());

    await emptyStageCard.click();
    await page.waitForTimeout(500);

    // ── Step 2: Verify empty state message in DAG area ──
    // The DAG area should show a placeholder message
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'))
      .or(page.locator('[class*="stage-canvas"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // Verify empty state text: "No nodes in this stage" or Chinese equivalent
    const emptyStateText = dagArea.locator("text=/No nodes in this stage|此阶段暂无节点/");
    await expect(emptyStateText).toBeVisible({ timeout: 3000 });

    // Verify the message suggests adding nodes in the editor
    // The hint can appear in Chinese or English
    const suggestionText = dagArea.locator(
      "text=/Add nodes|添加节点|editor|编辑器/",
    );
    await expect(suggestionText).toBeVisible({ timeout: 3000 });

    // Verify no ReactFlow nodes are rendered (empty DAG)
    const dagNodes = page.locator(".react-flow__node");
    const nodeCount = await dagNodes.count();
    expect(nodeCount).toBe(0);

    // ── Step 3: Switch to the non-empty stage ──
    const nonEmptyStageCard = page
      .getByTestId(`stage-card-${nonEmptyStage.id}`)
      .or(page.locator(`[data-stage-id="${nonEmptyStage.id}"]`))
      .or(page.getByText("Active Stage").first());

    await nonEmptyStageCard.click();
    await page.waitForTimeout(500);

    // ── Step 4: Verify DAG shows nodes normally ──
    // The empty state message should disappear
    await expect(emptyStateText).not.toBeVisible();

    // ReactFlow nodes should now be rendered
    const reactFlowNodes = page.locator(".react-flow__node");
    await expect(reactFlowNodes.first()).toBeVisible({ timeout: 3000 });

    const actualNodeCount = await reactFlowNodes.count();
    expect(actualNodeCount).toBeGreaterThanOrEqual(3);

    // Verify specific node titles are visible
    await expect(
      page.locator("text=/Task Alpha|Task Beta|Task Gamma/")
    ).toBeVisible({ timeout: 3000 });
  });
});
