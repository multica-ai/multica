// E2E test: "Open in editor" button navigates to workflow editor.
//
// Opens the detail panel for a node in the overview page, then clicks
// the "Open in editor" button. Verifies:
//   1. Navigation to `/{slug}/workflows/{id}` (editor page)
//   2. The editor page loads successfully
//   3. The same workflow is displayed in the editor
//
// Depends on: backend workflow + stage + node API, frontend overview +
// editor pages.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Open in Editor Link", () => {
  test("clicking 'Open in editor' navigates to the workflow editor", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Open Editor Test " + Date.now()
    );

    const stage1 = await seededApi.createWorkflowStage(
      workflow.id,
      "Design",
      1
    );

    // Create a node to click on
    await seededApi.createWorkflowNode(workflow.id, {
      title: "UI Mockup",
      stage_id: stage1.id,
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // Wait for the page to load
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"));
    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Click on a node to open the detail panel ──
    const nodeElement = dagArea
      .locator(".react-flow__node")
      .or(page.locator('[class*="workflow-node"]'))
      .or(page.getByTestId(/^workflow-node-/))
      .first();

    await expect(nodeElement).toBeVisible({ timeout: 5000 });
    await nodeElement.click();

    // ── Step 2: Verify the detail panel opens ──
    const detailPanel = page
      .getByTestId("detail-panel")
      .or(page.locator('[role="dialog"]'))
      .or(page.locator('[class*="detail-panel"]'))
      .or(page.locator('[aria-label*="detail"i]'))
      .first();

    await expect(detailPanel).toBeVisible({ timeout: 3000 });

    // ── Step 3: Click "Open in editor" button ──
    const openInEditorButton = detailPanel
      .getByRole("button", { name: /open in editor|在编辑器中打开/ })
      .or(detailPanel.locator('button:has-text("Open in editor")'))
      .or(detailPanel.locator('button:has-text("在编辑器中打开")'))
      .or(detailPanel.locator('a', { hasText: /open in editor|在编辑器中打开/ }))
      .first();

    await expect(openInEditorButton).toBeVisible({ timeout: 3000 });

    // ── Step 4: Click and wait for navigation ──
    await Promise.all([
      page.waitForURL(`**/workflows/${workflow.id}`, { timeout: 10000 }),
      openInEditorButton.click(),
    ]);

    // ── Step 5: Verify the editor page loads successfully ──
    // URL should be on the editor page (without /overview suffix)
    expect(page.url()).toContain(`/workflows/${workflow.id}`);
    expect(page.url()).not.toContain("/overview");

    // ── Step 6: Verify the editor page content ──
    // The editor should show the workflow canvas/editor area
    const editorCanvas = page
      .getByTestId("editor-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="workflow-editor"]'))
      .or(page.locator('[class*="editor"]'))
      .first();

    await expect(editorCanvas).toBeVisible({ timeout: 5000 });

    // Verify the workflow title or identifier is displayed
    const workflowTitle = page
      .getByText(/E2E Open Editor Test/)
      .or(page.locator("h1"))
      .first();

    await expect(workflowTitle).toBeVisible({ timeout: 3000 });
  });
});
