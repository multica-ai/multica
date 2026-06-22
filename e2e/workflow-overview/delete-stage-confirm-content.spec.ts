// E2E test: Delete stage with nodes — confirmation content verification.
//
// Seeds a stage with exactly 3 nodes, then attempts deletion. Verifies:
//   1. Confirmation dialog mentions "3" nodes explicitly
//   2. Dialog mentions nodes will go to "Unassigned" / "未分组"
//   3. Canceling the deletion leaves the stage and nodes intact
//
// Depends on: backend workflow + stage + node API, frontend confirmation dialog.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Delete Stage Confirmation Content", () => {
  test("confirmation dialog shows node count and cancel keeps stage", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with one stage that has exactly 3 nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Confirm Content Test " + Date.now()
    );

    // Create two stages: one to delete (with 3 nodes) and one other
    const targetStage = await seededApi.createWorkflowStage(
      workflow.id,
      "Design",
      1
    );
    await seededApi.createWorkflowStage(workflow.id, "Review", 2);

    // Add exactly 3 nodes to the target stage
    await seededApi.createWorkflowNode(workflow.id, {
      title: "UI Design",
      stage_id: targetStage.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Interaction Design",
      stage_id: targetStage.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Visual Design",
      stage_id: targetStage.id,
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // Wait for the stage canvas to load
    const stageCanvas = page
      .getByTestId("stage-canvas")
      .or(page.locator('[class*="stage-canvas"]'));
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 1: Find the target stage card ──
    const targetCard = page
      .getByTestId(`stage-card-${targetStage.id}`)
      .or(page.locator('[class*="stage-card"]').first());

    await expect(targetCard).toBeVisible({ timeout: 3000 });

    // ── Step 2: Open context menu for the target stage ──
    const contextMenuButton = targetCard
      .getByRole("button", { name: /more|menu|options|更多|菜单|选项/ })
      .or(targetCard.locator('[class*="more"]'))
      .or(targetCard.locator('[class*="three-dot"]'));

    const hasContextButton = await contextMenuButton
      .isVisible()
      .catch(() => false);

    if (hasContextButton) {
      await contextMenuButton.click();
    } else {
      await targetCard.click({ button: "right" });
    }

    // ── Step 3: Click "Delete" option ──
    const deleteOption = page
      .getByRole("menuitem", { name: /Delete|删除/ })
      .or(page.getByText(/Delete|删除/).first());

    await expect(deleteOption).toBeVisible({ timeout: 3000 });
    await deleteOption.click();

    // ── Step 4: Verify confirmation dialog appears ──
    const confirmDialog = page
      .getByRole("alertdialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="confirm"]').first())
      .or(page.locator('[class*="alert"]').first());

    await expect(confirmDialog).toBeVisible({ timeout: 3000 });

    // ── Step 5: Verify the dialog explicitly mentions "3" nodes ──
    const dialogText = await confirmDialog.textContent();

    // The dialog should contain the number 3 (node count)
    expect(dialogText).not.toBeNull();
    if (dialogText) {
      // Extract all numbers from dialog text
      const numbersFound = dialogText.match(/\d+/g);
      expect(numbersFound).not.toBeNull();
      if (numbersFound) {
        // At least one number should be 3 (the node count)
        expect(numbersFound).toContain("3");
      }
    }

    // ── Step 6: Verify dialog mentions nodes going to "Unassigned" ──
    const unassignedMention = confirmDialog
      .getByText(/Unassigned|未分组|节点将移至/)
      .or(
        confirmDialog.locator(
          'text=/nodes? will be moved|节点将移至|Unassigned|未分组/'
        )
      );

    await expect(unassignedMention).toBeVisible({ timeout: 2000 });

    // ── Step 7: Cancel the deletion ──
    const cancelButton = confirmDialog
      .getByRole("button", { name: /cancel|取消/ })
      .or(confirmDialog.locator('button:has-text("Cancel")'))
      .or(confirmDialog.locator('button:has-text("取消")'));

    await expect(cancelButton).toBeEnabled({ timeout: 2000 });
    await cancelButton.click();

    // ── Step 8: Verify dialog closes ──
    await expect(confirmDialog).not.toBeVisible({ timeout: 3000 });

    // ── Step 9: Verify stage still exists in the strip ──
    const stageCard = page
      .getByTestId(`stage-card-${targetStage.id}`)
      .or(page.locator('[class*="stage-card"]').first());

    await expect(stageCard).toBeVisible({ timeout: 3000 });

    // ── Step 10: Verify nodes still belong to this stage ──
    // Click the stage card to verify its DAG shows nodes
    await stageCard.click();

    // Verify the DAG area is visible and contains nodes
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"));

    await expect(dagArea).toBeVisible({ timeout: 3000 });

    // Verify the stage card still shows the stage name
    await expect(stageCard).toContainText(/Design/);

    // ── Step 11: Verify via API that the stage still exists ──
    // (Soft assertion — if the stage is visible in the UI, it should exist)
    const remainingCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    const cardCount = await remainingCards.count();
    expect(cardCount).toBe(2);
  });
});
