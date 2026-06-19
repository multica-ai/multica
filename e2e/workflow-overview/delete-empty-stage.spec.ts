// E2E test: Delete empty stage (no nodes) — simplified confirmation.
//
// Seeds a workflow with an empty stage (no nodes assigned), opens the
// context menu, clicks "Delete", and verifies:
//   1. Confirmation dialog does NOT mention moving nodes
//   2. Stage card is removed from the strip
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Delete Empty Stage", () => {
  test("deletes an empty stage without node move mention", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with an empty stage ──
    const workflow = await seededApi.createWorkflow(
      "E2E Delete Empty Stage Test " + Date.now()
    );

    // Create stages — one with nodes, one empty
    const stageWithNodes = await seededApi.createWorkflowStage(workflow.id, "Draft", 1);
    const emptyStage = await seededApi.createWorkflowStage(workflow.id, "Empty", 2);
    await seededApi.createWorkflowStage(workflow.id, "Review", 3);

    // Add a node only to the first stage, leaving "Empty" with zero nodes
    await seededApi.createWorkflowNode(workflow.id, {
      title: "草稿节点",
      stage_id: stageWithNodes.id,
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

    // ── Step 1: Find the empty stage card ──
    const emptyStageCard = page
      .getByTestId(`stage-card-${emptyStage.id}`)
      .or(page.getByText("Empty").first())
      .or(page.locator('[class*="stage-card"]').nth(1));

    await expect(emptyStageCard).toBeVisible({ timeout: 3000 });

    // ── Step 2: Open context menu on the empty stage ──
    const contextMenuButton = emptyStageCard
      .getByRole("button", { name: /more|menu|options|更多|菜单|选项/ })
      .or(emptyStageCard.locator('[class*="more"]'))
      .or(emptyStageCard.locator('[class*="three-dot"]'));

    const hasContextButton = await contextMenuButton.isVisible().catch(() => false);

    if (hasContextButton) {
      await contextMenuButton.click();
    } else {
      await emptyStageCard.click({ button: "right" });
    }

    // ── Step 3: Click "Delete" option ──
    const deleteOption = page
      .getByRole("menuitem", { name: /Delete|删除/ })
      .or(page.getByText(/Delete|删除/).first());

    await expect(deleteOption).toBeVisible({ timeout: 3000 });
    await deleteOption.click();

    // ── Step 4: Check whether a confirmation dialog appears ──
    const confirmDialog = page
      .getByRole("alertdialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="confirm"]').first());

    const hasConfirmDialog = await confirmDialog.isVisible().catch(() => false);

    if (hasConfirmDialog) {
      // If confirmation dialog appears, verify it does NOT mention moving nodes
      const dialogText = (await confirmDialog.textContent()) ?? "";

      // The dialog should NOT contain node-move language
      const hasNodeMoveMention =
        /node|节点|移至|move|unassigned|未分组/i.test(dialogText);
      expect(hasNodeMoveMention).toBe(false);

      // Confirm deletion
      const confirmButton = confirmDialog
        .getByRole("button", { name: /delete|confirm|remove|删除|确定|移除/ })
        .or(confirmDialog.locator('button[class*="danger"]'))
        .or(confirmDialog.locator('button[class*="destructive"]'))
        .or(confirmDialog.locator('button:has-text("Delete")'))
        .or(confirmDialog.locator('button:has-text("删除")'));

      await expect(confirmButton).toBeEnabled({ timeout: 2000 });
      await confirmButton.click();
    }
    // If no confirmation dialog, the delete may be instant.
    // Allow a brief moment for the UI to process the action.

    // ── Step 5: Verify stage card is removed from the strip ──
    // The empty stage card should no longer be visible
    await expect(
      page.getByTestId(`stage-card-${emptyStage.id}`)
    ).not.toBeVisible({ timeout: 5000 });

    // ── Step 6: Verify remaining cards — should be 2 (Draft + Review) ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    const cardCount = await stageCards.count();
    expect(cardCount).toBe(2);

    // ── Step 7: Verify the stage with nodes still exists ──
    const draftCard = page
      .getByTestId(`stage-card-${stageWithNodes.id}`)
      .or(page.getByText("Draft").first());

    await expect(draftCard).toBeVisible({ timeout: 2000 });
  });
});
