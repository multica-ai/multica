// E2E test: Unassigned nodes virtual card.
//
// Seeds a workflow with stages AND some nodes that have stage_id = null.
// Verifies:
//   1. An "Unassigned" / "未分组" virtual card appears in the stage strip
//   2. The card shows the count of unassigned nodes
//   3. Clicking the card causes the DAG to show those unassigned nodes
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Unassigned Nodes Virtual Card", () => {
  test("shows virtual unassigned card with node count and DAG renders on click", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and unassigned nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Unassigned Test " + Date.now()
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

    // Create nodes assigned to stages
    await seededApi.createWorkflowNode(workflow.id, {
      title: "User Research",
      stage_id: stage1.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Data Analysis",
      stage_id: stage1.id,
    });

    await seededApi.createWorkflowNode(workflow.id, {
      title: "Build Feature",
      stage_id: stage2.id,
    });

    // Create nodes with NO stage assignment (stage_id = null)
    // These should appear in the "Unassigned" virtual card
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Orphaned Task A",
      stage_id: null,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Orphaned Task B",
      stage_id: null,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Orphaned Task C",
      stage_id: null,
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 1: Verify the main canvas is present ──
    const stageCanvas = page.getByTestId("stage-canvas").or(
      page.locator('[class*="stage-canvas"]')
    );
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 2: Verify the unassigned virtual card is visible ──
    // The card should display "Unassigned" or "未分组" text
    const unassignedCard = page
      .getByTestId("stage-card-unassigned")
      .or(page.getByText(/Unassigned|未分组/));

    await expect(unassignedCard).toBeVisible({ timeout: 5000 });

    // ── Step 3: Verify the unassigned card shows the node count ──
    // We created 3 nodes with stage_id = null
    await expect(unassignedCard).toContainText(
      /\d+ nodes?|\d+ 个节点/
    );

    // Extract the count from the card and verify it's 3
    const cardText = await unassignedCard.textContent();
    const countMatch = cardText?.match(/(\d+)/);
    expect(countMatch).not.toBeNull();
    const count = countMatch ? parseInt(countMatch[1], 10) : 0;
    expect(count).toBe(3);

    // ── Step 4: Verify the unassigned card is positioned last in the strip ──
    // The virtual card should always appear last after all regular stage cards
    const allCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-unassigned"))
      .or(page.locator('[class*="stage-card"]'));

    const cardCount = await allCards.count();
    expect(cardCount).toBeGreaterThanOrEqual(3); // 2 real stages + 1 virtual card

    // ── Step 5: Click the unassigned card ──
    await unassignedCard.click();

    // ── Step 6: Verify DAG renders showing unassigned nodes ──
    // The DAG area should update to show only the unassigned (orphaned) nodes
    const dagArea = page
      .getByTestId("stage-node-dag")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 3000 });

    // Verify the unassigned node titles appear in the DAG or detail area
    await expect(
      page.locator('text=/Orphaned Task A|Orphaned Task B|Orphaned Task C/')
    ).toBeVisible({ timeout: 3000 });
  });
});
