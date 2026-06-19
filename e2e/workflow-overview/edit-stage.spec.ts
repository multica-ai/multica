// E2E test: Edit stage name via context menu.
//
// Seeds a workflow with stages, opens the context menu on the first stage
// card, clicks "Edit", renames the stage in the pre-filled dialog, and
// verifies the card updates with the new name.
//
// Depends on: backend workflow + stage API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Edit Stage", () => {
  test("renames a stage via context menu edit option", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages ──
    const workflow = await seededApi.createWorkflow(
      "E2E Edit Stage Test " + Date.now()
    );

    const stage1 = await seededApi.createWorkflowStage(workflow.id, "Research", 1);
    await seededApi.createWorkflowStage(workflow.id, "Draft", 2);
    await seededApi.createWorkflowStage(workflow.id, "Review", 3);

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

    // ── Step 1: Find the first stage card and open its context menu ──
    const firstCard = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.getByTestId(/^stage-card-/).first())
      .or(page.locator('[class*="stage-card"]').first());

    await expect(firstCard).toBeVisible({ timeout: 3000 });

    // Open context menu — try the three-dot button first, then right-click
    const contextMenuButton = firstCard
      .getByRole("button", { name: /more|menu|options|更多|菜单|选项/ })
      .or(firstCard.locator('[class*="more"]'))
      .or(firstCard.locator('[class*="three-dot"]'))
      .or(firstCard.locator('[class*="context"]'));

    const hasContextButton = await contextMenuButton.isVisible().catch(() => false);

    if (hasContextButton) {
      await contextMenuButton.click();
    } else {
      // Fallback: right-click the card to open context menu
      await firstCard.click({ button: "right" });
    }

    // ── Step 2: Click "Edit" option in the context menu ──
    const editOption = page
      .getByRole("menuitem", { name: /Edit|编辑|重命名/ })
      .or(page.getByText(/Edit|编辑|重命名/).first());

    await expect(editOption).toBeVisible({ timeout: 3000 });
    await editOption.click();

    // ── Step 3: Verify dialog opens pre-filled with current name ──
    const dialog = page
      .getByTestId("stage-create-dialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="dialog"]').first());

    await expect(dialog).toBeVisible({ timeout: 3000 });

    // Dialog should have a title matching edit pattern
    await expect(
      dialog.locator('text=/Edit Stage|编辑阶段|重命名阶段/')
    ).toBeVisible({ timeout: 2000 });

    // Name input should exist and be pre-filled with "Research"
    const nameInput = dialog
      .getByLabel(/name|名称|阶段名称/)
      .or(dialog.locator('input[type="text"]').first());

    await expect(nameInput).toBeVisible({ timeout: 2000 });

    // Verify the input is pre-filled with the current stage name
    const currentValue = await nameInput.inputValue();
    expect(currentValue).toBe("Research");

    // ── Step 4: Change name and save ──
    const newName = "需求分析";
    await nameInput.fill(newName);

    const saveButton = dialog
      .getByRole("button", { name: /save|confirm|update|保存|确定|更新/ })
      .or(dialog.locator('button[type="submit"]').first());

    await expect(saveButton).toBeEnabled({ timeout: 2000 });
    await saveButton.click();

    // ── Step 5: Verify dialog closes ──
    await expect(dialog).not.toBeVisible({ timeout: 5000 });

    // ── Step 6: Verify the stage card now shows the new name ──
    // The first card (or the card for stage1) should now display "需求分析"
    const updatedCard = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.getByTestId(/^stage-card-/).first())
      .or(page.locator('[class*="stage-card"]').first());

    await expect(updatedCard).toContainText(newName, { timeout: 3000 });

    // Verify the old name is no longer present on the card
    await expect(updatedCard).not.toContainText("Research");

    // ── Step 7: Verify card count remains unchanged ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    const cardCount = await stageCards.count();
    expect(cardCount).toBe(3);
  });
});
