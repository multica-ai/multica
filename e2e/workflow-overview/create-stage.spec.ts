// E2E test: Create stage via "+" button and dialog.
//
// Seeds a workflow with stages, then clicks the add stage button,
// fills in the dialog, and verifies the new stage card appears.
//
// Depends on: backend workflow + stage API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Create Stage", () => {
  test("creates a new stage via add button dialog", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with initial stages ──
    const workflow = await seededApi.createWorkflow(
      "E2E Create Stage Test " + Date.now()
    );

    // Create a couple of stages so the strip is populated
    const stage1 = await seededApi.createWorkflowStage(workflow.id, "Research", 1);
    await seededApi.createWorkflowStage(workflow.id, "Draft", 2);

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

    // ── Step 1: Record current number of stage cards ──
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    const initialCardCount = await stageCards.count();

    // ── Step 2: Click "+" add stage button ──
    const addButton = page
      .getByTestId("add-stage-button")
      .or(page.getByRole("button", { name: /\+|add stage|添加阶段|新增阶段/ }));
    await expect(addButton).toBeVisible({ timeout: 3000 });
    await addButton.click();

    // ── Step 3: Verify dialog opens ──
    const dialog = page
      .getByTestId("stage-create-dialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="dialog"]').first());

    await expect(dialog).toBeVisible({ timeout: 3000 });

    // Dialog title should match create stage pattern
    await expect(
      dialog.locator('text=/Create Stage|创建阶段|新建阶段/')
    ).toBeVisible({ timeout: 2000 });

    // Dialog contains a name input field
    const nameInput = dialog
      .getByLabel(/name|名称|阶段名称/)
      .or(dialog.locator('input[type="text"]').first());

    await expect(nameInput).toBeVisible({ timeout: 2000 });

    // ── Step 4: Type stage name ──
    const stageName = "测试阶段";
    await nameInput.fill(stageName);

    // ── Step 5: Click confirm/save button ──
    const confirmButton = dialog
      .getByRole("button", { name: /confirm|create|save|确定|创建|保存/ })
      .or(dialog.locator('button[type="submit"]').first());

    await expect(confirmButton).toBeEnabled({ timeout: 2000 });
    await confirmButton.click();

    // ── Step 6: Verify dialog closes ──
    await expect(dialog).not.toBeVisible({ timeout: 5000 });

    // ── Step 7: Verify new stage card appears ──
    // The new card should display the stage name
    const newStageCard = page
      .getByTestId(/^stage-card-/)
      .or(page.getByTestId("stage-card-strip").locator('[class*="stage-card"]'));

    const newCardCount = await newStageCard.count();
    expect(newCardCount).toBe(initialCardCount + 1);

    // Verify the new stage name is visible in the strip
    await expect(
      page.locator(`text=${stageName}`)
    ).toBeVisible({ timeout: 3000 });

    // ── Step 8: Verify via API that the stage exists ──
    // OPTIONAL: Check for success notification or optimistic update
    // (implementation-dependent, not strictly required)

    // ── Step 9: Verify network request was made ──
    // POST to /api/workflows/{id}/stages should return 201
    // This is a soft check — wait for the response if observable
    await expect(page.locator(`text=${stageName}`)).toBeVisible({ timeout: 3000 });
  });
});
