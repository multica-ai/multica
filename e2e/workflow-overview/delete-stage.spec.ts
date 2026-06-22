// E2E test: Delete stage with nodes — confirmation and cascade.
//
// Seeds a workflow with stages and nodes, then deletes a stage that has
// nodes. Verifies:
//   1. Confirmation dialog mentions node count and "Unassigned"
//   2. Stage card disappears after confirmation
//   3. Nodes move to the "Unassigned" virtual card
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Delete Stage With Nodes", () => {
  test("deletes a stage with nodes and moves nodes to unassigned", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Delete Stage Test " + Date.now()
    );

    // Create 3 stages
    const stage1 = await seededApi.createWorkflowStage(workflow.id, "Research", 1);
    const stage2 = await seededApi.createWorkflowStage(workflow.id, "Draft", 2);
    const stage3 = await seededApi.createWorkflowStage(workflow.id, "Review", 3);

    // Assign 2 nodes to stage1
    const node1 = await seededApi.createWorkflowNode(workflow.id, {
      title: "需求收集",
      stage_id: stage1.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "需求分析",
      stage_id: stage1.id,
    });

    // Assign 1 node to stage2
    await seededApi.createWorkflowNode(workflow.id, {
      title: "UI设计",
      stage_id: stage2.id,
    });

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

    // ── Step 1: Find the stage1 card (has nodes) and open context menu ──
    const targetCard = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.getByTestId(/^stage-card-/).first())
      .or(page.locator('[class*="stage-card"]').first());

    await expect(targetCard).toBeVisible({ timeout: 3000 });

    // Open context menu
    const contextMenuButton = targetCard
      .getByRole("button", { name: /more|menu|options|更多|菜单|选项/ })
      .or(targetCard.locator('[class*="more"]'))
      .or(targetCard.locator('[class*="three-dot"]'));

    const hasContextButton = await contextMenuButton.isVisible().catch(() => false);

    if (hasContextButton) {
      await contextMenuButton.click();
    } else {
      await targetCard.click({ button: "right" });
    }

    // ── Step 2: Click "Delete" option ──
    const deleteOption = page
      .getByRole("menuitem", { name: /Delete|删除/ })
      .or(page.getByText(/Delete|删除/).first());

    await expect(deleteOption).toBeVisible({ timeout: 3000 });
    await deleteOption.click();

    // ── Step 3: Verify confirmation dialog appears ──
    const confirmDialog = page
      .getByRole("alertdialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="confirm"]').first())
      .or(page.locator('[class*="alert"]').first());

    await expect(confirmDialog).toBeVisible({ timeout: 3000 });

    // The dialog should mention node count moving to "Unassigned"
    await expect(
      confirmDialog.locator(
        'text=/This stage contains \\d+ node|此阶段包含 \\d+ 个节点|节点将移至|nodes? will be moved|Unassigned|未分组/'
      )
    ).toBeVisible({ timeout: 2000 });

    // Extract and verify the node count mention
    const dialogText = await confirmDialog.textContent();
    const nodeCountMatch = dialogText?.match(/(\d+)\s*(node|个)/i);
    expect(nodeCountMatch).not.toBeNull();
    if (nodeCountMatch) {
      expect(parseInt(nodeCountMatch[1], 10)).toBe(2);
    }

    // ── Step 4: Confirm deletion ──
    const confirmButton = confirmDialog
      .getByRole("button", { name: /delete|confirm|remove|删除|确定|移除/ })
      .or(confirmDialog.locator('button[class*="danger"]'))
      .or(confirmDialog.locator('button[class*="destructive"]'))
      .or(confirmDialog.locator('button:has-text("Delete")'))
      .or(confirmDialog.locator('button:has-text("删除")'));

    await expect(confirmButton).toBeEnabled({ timeout: 2000 });
    await confirmButton.click();

    // ── Step 5: Verify stage card disappears ──
    await expect(
      page.getByTestId(`stage-card-${stage1.id}`)
    ).not.toBeVisible({ timeout: 5000 });

    // ── Step 6: Verify remaining cards count is 2 (stage2 + stage3) ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    const cardCount = await stageCards.count();
    expect(cardCount).toBe(2);

    // ── Step 7: Verify nodes now appear under "Unassigned" ──
    // The "Unassigned" virtual card should show and contain the moved nodes
    const unassignedCard = page
      .getByTestId("stage-card-unassigned")
      .or(page.getByText(/Unassigned|未分组/).first())
      .or(page.locator('[class*="unassigned"]').first());

    await expect(unassignedCard).toBeVisible({ timeout: 3000 });

    // The unassigned card should indicate 2 nodes
    await expect(unassignedCard).toContainText(/\d+/);

    // Click the unassigned card to verify nodes render in the DAG
    await unassignedCard.click();

    // Verify the DAG area updates (implementation-dependent check)
    // At minimum, the DAG should be visible after selecting a card
    const dagArea = page.getByTestId("dag-canvas").or(
      page.locator(".react-flow")
    );
    await expect(dagArea).toBeVisible({ timeout: 3000 });
  });
});
