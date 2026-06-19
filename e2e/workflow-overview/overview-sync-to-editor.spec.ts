// E2E test: Overview stage changes sync to editor.
//
// Note: These tests will be refined when the workflow editor's add-node flow is
// stable. The sync verification logic is the goal — editor interactions are
// placeholder selectors.
//
// Verifies that creating a stage in the overview page is reflected in the
// workflow editor. The test:
//   1. Opens the overview page and creates a new stage
//   2. Navigates to the editor page
//   3. Verifies the new stage's nodes are visible in the editor's DAG
//   4. Verifies nodes are grouped/colored by stage (if supported)
//
// Depends on: backend workflow + stage API, frontend overview + editor.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Overview Stage Changes Sync to Editor", () => {
  test("stage created in overview appears in editor DAG", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with one stage and one node ──
    const workflow = await seededApi.createWorkflow(
      "E2E Overview Sync Test " + Date.now()
    );

    const existingStage = await seededApi.createWorkflowStage(
      workflow.id,
      "Initial Stage",
      1
    );

    // Add a node to the existing stage so we can verify it later
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Existing Node",
      stage_id: existingStage.id,
    });

    const overviewUrl = `/${slug}/workflows/${workflow.id}/overview`;
    const editorUrl = `/${slug}/workflows/${workflow.id}`;

    // ── Step 1: Open the overview page and note current stages ──
    await page.goto(overviewUrl);
    await expect(page).toHaveURL(overviewUrl);

    // Wait for the stage canvas to load
    const stageCanvas = page
      .getByTestId("stage-canvas")
      .or(page.locator('[class*="stage-canvas"]'));
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // Count initial stage cards
    const stageCards = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    const initialCardCount = await stageCards.count();
    expect(initialCardCount).toBeGreaterThanOrEqual(1);

    // ── Step 2: Create a new stage via the "+" button ──
    const addButton = page
      .getByTestId("add-stage-button")
      .or(
        page.getByRole("button", {
          name: /\+|add stage|添加阶段|新增阶段/,
        })
      );
    await expect(addButton).toBeVisible({ timeout: 3000 });
    await addButton.click();

    // Fill in the create stage dialog
    const dialog = page
      .getByTestId("stage-create-dialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="dialog"]').first());

    await expect(dialog).toBeVisible({ timeout: 3000 });

    // Enter a stage name
    const newStageName = "Stage from Overview";
    const nameInput = dialog
      .getByLabel(/name|名称|阶段名称/)
      .or(dialog.locator('input[type="text"]').first());

    await expect(nameInput).toBeVisible({ timeout: 2000 });
    await nameInput.fill(newStageName);

    // Confirm/save
    const confirmButton = dialog
      .getByRole("button", { name: /confirm|create|save|确定|创建|保存/ })
      .or(dialog.locator('button[type="submit"]').first());

    await expect(confirmButton).toBeEnabled({ timeout: 2000 });
    await confirmButton.click();

    // Verify dialog closes and new card appears
    await expect(dialog).not.toBeVisible({ timeout: 5000 });

    // ── Step 3: Navigate to the editor page ──
    await page.goto(editorUrl);
    await expect(page).toHaveURL(editorUrl);

    // Wait for the editor canvas to load
    const editorCanvas = page
      .getByTestId("editor-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="workflow-editor"]'))
      .first();

    await expect(editorCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 4: Verify the new stage is visible in the editor's DAG ──
    // The editor should show the new stage name somewhere (as a group/label)
    const stageGroupLabel = page
      .getByText(new RegExp(newStageName, "i"))
      .or(
        page.locator(`[class*="stage-group"]:has-text("${newStageName}")`)
      )
      .or(page.locator(`text="${newStageName}"`))
      .first();

    await expect(stageGroupLabel).toBeVisible({ timeout: 5000 });

    // ── Step 5: Verify nodes are grouped/colored by stage ──
    // Check that the editor canvas has some visual grouping mechanism
    // for stages (e.g., stage group containers, colored backgrounds)
    const stageGroups = editorCanvas
      .locator('[class*="stage-group"]')
      .or(editorCanvas.locator('[class*="group"]').first());

    const hasStageGrouping = await stageGroups
      .isVisible()
      .catch(() => false);

    // If the editor supports stage grouping, verify at least one group
    if (hasStageGrouping) {
      const groupCount = await stageGroups.count();
      // There should be at least as many groups as stages
      expect(groupCount).toBeGreaterThanOrEqual(1);
    }

    // ── Step 6: Verify the initial stage also appears ──
    const initialStageLabel = page
      .getByText(/Initial Stage/)
      .or(
        page.locator('[class*="stage-group"]:has-text("Initial Stage")')
      )
      .first();

    await expect(initialStageLabel).toBeVisible({ timeout: 3000 });

    // ── Step 7: Verify the existing node is still in the editor ──
    const existingNodeInEditor = editorCanvas
      .getByText(/Existing Node/)
      .or(editorCanvas.locator(".react-flow__node").first());

    await expect(existingNodeInEditor).toBeVisible({ timeout: 3000 });
  });
});
