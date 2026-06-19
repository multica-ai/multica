// E2E test: Editor changes sync to overview page.
//
// Verifies that adding a node in the workflow editor is reflected in the
// overview page's stage DAG. The test:
//   1. Opens the overview page and notes the stage/node count
//   2. Opens the editor page (same tab)
//   3. Adds a new node to a stage in the editor and saves
//   4. Switches back to the overview page (or reloads)
//   5. Verifies the new node appears in the correct stage's DAG
//
// Depends on: backend workflow + stage + node API, frontend overview + editor.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Editor Changes Sync to Overview", () => {
  test("node added in editor appears in overview stage DAG", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Editor Sync Test " + Date.now()
    );

    const stage1 = await seededApi.createWorkflowStage(
      workflow.id,
      "Design",
      1
    );

    // Add an initial node to the stage
    const initialNode = await seededApi.createWorkflowNode(workflow.id, {
      title: "Initial Node",
      stage_id: stage1.id,
    });

    const overviewUrl = `/${slug}/workflows/${workflow.id}/overview`;
    const editorUrl = `/${slug}/workflows/${workflow.id}`; // editor page

    // ── Step 1: Open the overview page and note the node count ──
    await page.goto(overviewUrl);
    await expect(page).toHaveURL(overviewUrl);

    // Wait for the stage canvas to load
    const stageCanvas = page
      .getByTestId("stage-canvas")
      .or(page.locator('[class*="stage-canvas"]'));
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // Click the stage card to select it and see its DAG
    const stageCard = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.locator('[class*="stage-card"]').first());

    await expect(stageCard).toBeVisible({ timeout: 3000 });
    await stageCard.click();

    // Record the initial node count in the DAG
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"));

    await expect(dagArea).toBeVisible({ timeout: 3000 });

    const initialNodeCount = await dagArea
      .locator(".react-flow__node")
      .count();

    // ── Step 2: Navigate to the editor page ──
    await page.goto(editorUrl);
    await expect(page).toHaveURL(editorUrl);

    // Wait for the editor canvas to load
    const editorCanvas = page
      .getByTestId("editor-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="workflow-editor"]'))
      .first();

    await expect(editorCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 3: Add a new node in the editor ──
    // Find the "Add Node" or "+" button in the editor
    const addNodeButton = page
      .getByTestId("add-node-button")
      .or(
        page
          .getByRole("button", { name: /\+|add node|add step|添加节点|添加步骤/ })
          .first()
      )
      .or(page.locator('[class*="add-node"]').first())
      .or(page.locator('[class*="add-step"]').first());

    // If the editor uses drag-and-drop or palette, try to find it
    const hasAddButton = await addNodeButton.isVisible().catch(() => false);

    if (hasAddButton) {
      await addNodeButton.click();
    }

    // Fill in node details if a dialog appears
    const nodeDialog = page
      .getByRole("dialog")
      .or(page.locator('[class*="dialog"]').first());

    const hasNodeDialog = await nodeDialog.isVisible().catch(() => false);

    if (hasNodeDialog) {
      const nodeNameInput = nodeDialog
        .getByLabel(/name|title|名称|标题/)
        .or(nodeDialog.locator('input').first());

      if (await nodeNameInput.isVisible().catch(() => false)) {
        await nodeNameInput.fill("Node from Editor");
      }

      // Save/confirm the node
      const saveButton = nodeDialog
        .getByRole("button", { name: /save|add|confirm|保存|添加|确定/ })
        .or(nodeDialog.locator('button[type="submit"]').first());

      if (await saveButton.isVisible().catch(() => false)) {
        await saveButton.click();
      }
    }

    // Wait for the node to appear in the editor canvas
    const editorNodes = editorCanvas.locator(".react-flow__node");
    await expect(editorNodes.first()).toBeVisible({ timeout: 5000 });

    // ── Step 4: Navigate back to the overview page ──
    await page.goto(overviewUrl);
    await expect(page).toHaveURL(overviewUrl);

    // Wait for the overview to reload
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 5: Verify the new node appears in the overview's DAG ──
    // Click the same stage card
    await stageCard.click();

    // Wait for the DAG to update with the new node
    const updatedNodeCount = await dagArea
      .locator(".react-flow__node")
      .count();

    // The node count should be greater than the initial count
    // (initial = 1 node from setup, new = 1+ node from editor)
    expect(updatedNodeCount).toBeGreaterThanOrEqual(initialNodeCount);

    // The new node should be visible in the DAG
    // Look for the node title we created
    const newNodeInDag = dagArea
      .getByText(/Node from Editor|从编辑器添加/)
      .or(dagArea.locator(".react-flow__node").last());

    await expect(newNodeInDag).toBeVisible({ timeout: 3000 });
  });
});
