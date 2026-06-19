// E2E test: Node detail panel close methods.
//
// Verifies three ways to close the node detail panel:
//   1. Click the × close button
//   2. Click on the DAG background (not on a node)
//   3. Press the Escape key
//
// Each method should cause the panel to disappear and the node to deselect.
//
// Depends on: backend workflow + stage + node API, frontend overview page
// with detail panel implementation.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Node Detail Panel - Close Methods", () => {
  test("close panel via × button, DAG background click, and Escape key", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Detail Panel Close " + Date.now(),
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Processing",
      1,
    );

    const nodeA = await seededApi.createWorkflowNode(workflow.id, {
      title: "Task Alpha",
      stage_id: stage.id,
      position_x: 100,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    await seededApi.createWorkflowNode(workflow.id, {
      title: "Task Beta",
      stage_id: stage.id,
      position_x: 400,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`,
    );

    // ── Select the stage ──
    const stageCard = page
      .getByTestId(`stage-card-${stage.id}`)
      .or(page.locator(`[data-stage-id="${stage.id}"]`))
      .or(page.getByText("Processing").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Shared locators ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    const dagNodes = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node"),
    );

    // Helper: open panel by clicking the first DAG node
    const openPanel = async () => {
      await dagNodes.first().click();
      await page.waitForTimeout(500);
    };

    // Helper: get the detail panel locator
    const detailPanel = () =>
      page
        .getByTestId("node-detail-panel")
        .or(page.locator('[role="dialog"]'))
        .or(page.locator('[role="complementary"]'))
        .or(page.locator('[class*="detail-panel"]'))
        .or(page.locator('[class*="node-detail"]'))
        .or(page.locator('[class*="side-panel"]'));

    // ──────────────────────────────────────────────
    // Method 1: Close via × close button
    // ──────────────────────────────────────────────
    await test.step("Close via × button", async () => {
      await openPanel();
      await expect(detailPanel().first()).toBeVisible({ timeout: 3000 });

      // Find and click the close button
      const closeButton = detailPanel()
        .getByTestId("close-detail-panel")
        .or(page.locator('[data-testid="close-detail-panel"]'))
        .or(detailPanel().locator('[aria-label="Close"]'));

      await closeButton.first().click();
      await page.waitForTimeout(500);

      // Verify panel disappears
      await expect(detailPanel().first()).not.toBeVisible();

      // Verify node deselects
      const selectedAfterClose = await dagNodes.evaluateAll((nodes) => {
        return nodes.some(
          (n) =>
            n.classList.contains("selected") ||
            n.classList.contains("active") ||
            n.getAttribute("aria-selected") === "true",
        );
      });
      expect(selectedAfterClose).toBe(false);
    });

    // ──────────────────────────────────────────────
    // Method 2: Close via DAG background click
    // ──────────────────────────────────────────────
    await test.step("Close via DAG background click", async () => {
      await openPanel();
      await expect(detailPanel().first()).toBeVisible({ timeout: 3000 });

      // Click on the DAG background (the pane, not a node)
      const dagPane = dagArea
        .locator(".react-flow__pane")
        .or(page.locator(".react-flow__pane"))
        .or(dagArea);

      // Get the pane bounding box and click in the top-left margin area
      const paneBox = await dagPane.first().boundingBox();
      if (paneBox) {
        // Click near the top-left corner of the pane, away from any node
        await page.mouse.click(paneBox.x + 10, paneBox.y + 10);
      } else {
        // Fallback: click on the canvas background
        await dagPane.first().click({ position: { x: 10, y: 10 } });
      }
      await page.waitForTimeout(500);

      // Verify panel disappears
      await expect(detailPanel().first()).not.toBeVisible();

      // Verify node deselects
      const selectedAfterBg = await dagNodes.evaluateAll((nodes) => {
        return nodes.some(
          (n) =>
            n.classList.contains("selected") ||
            n.classList.contains("active") ||
            n.getAttribute("aria-selected") === "true",
        );
      });
      expect(selectedAfterBg).toBe(false);
    });

    // ──────────────────────────────────────────────
    // Method 3: Close via Escape key
    // ──────────────────────────────────────────────
    await test.step("Close via Escape key", async () => {
      await openPanel();
      await expect(detailPanel().first()).toBeVisible({ timeout: 3000 });

      // Press Escape key
      await page.keyboard.press("Escape");
      await page.waitForTimeout(500);

      // Verify panel disappears
      await expect(detailPanel().first()).not.toBeVisible();

      // Verify node deselects
      const selectedAfterEscape = await dagNodes.evaluateAll((nodes) => {
        return nodes.some(
          (n) =>
            n.classList.contains("selected") ||
            n.classList.contains("active") ||
            n.getAttribute("aria-selected") === "true",
        );
      });
      expect(selectedAfterEscape).toBe(false);
    });
  });
});
