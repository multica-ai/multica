// E2E test: Delete the last stage — empty state.
//
// Seeds a workflow with exactly 1 stage, then deletes it. Verifies:
//   1. Stage card disappears
//   2. Workflow returns to empty state with "No stages defined yet" message
//
// Depends on: backend workflow + stage API, frontend empty state.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Delete Last Stage", () => {
  test("deleting the only stage shows empty state", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with exactly 1 stage ──
    const workflow = await seededApi.createWorkflow(
      "E2E Delete Last Stage Test " + Date.now()
    );

    const onlyStage = await seededApi.createWorkflowStage(
      workflow.id,
      "Planning",
      1
    );

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

    // ── Step 1: Verify the stage card is visible initially ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    const initialCardCount = await stageCards.count();
    expect(initialCardCount).toBeGreaterThanOrEqual(1);

    // ── Step 2: Find the stage card and open context menu ──
    const targetCard = page
      .getByTestId(`stage-card-${onlyStage.id}`)
      .or(stageCards.first());

    await expect(targetCard).toBeVisible({ timeout: 3000 });

    // Open context menu
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

    // ── Step 4: Confirm deletion in the dialog ──
    const confirmDialog = page
      .getByRole("alertdialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="confirm"]').first());

    await expect(confirmDialog).toBeVisible({ timeout: 3000 });

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
      page.getByTestId(`stage-card-${onlyStage.id}`)
    ).not.toBeVisible({ timeout: 5000 });

    // ── Step 6: Verify empty state is shown ──
    // After the last stage is deleted, the workflow should show "No stages
    // defined yet" or equivalent empty state message
    const emptyState = page
      .getByTestId("empty-state")
      .or(page.getByText(/no stages? defined|no stages? yet|暂无阶段|还没有阶段/))
      .or(
        page.getByRole("heading", {
          name: /no stages?|暂无阶段/,
        })
      )
      .or(page.locator('[class*="empty-state"]'))
      .or(page.locator('[class*="empty"]').first());

    await expect(emptyState).toBeVisible({ timeout: 5000 });

    // ── Step 7: Verify no stage cards remain ──
    const remainingCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    const remainingCount = await remainingCards.count();
    expect(remainingCount).toBe(0);

    // ── Step 8: Verify "Add stage" button is still present ──
    // The empty state should allow the user to create a new stage
    const addButton = page
      .getByTestId("add-stage-button")
      .or(
        page.getByRole("button", {
          name: /\+|add stage|添加阶段|新增阶段/,
        })
      );

    await expect(addButton).toBeVisible({ timeout: 3000 });
  });
});
