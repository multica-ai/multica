// E2E test: Workflow Overview page shell structure.
//
// Verifies the three-zone layout of the workflow overview page:
//   1. Top: horizontal scrollable stage card strip
//   2. Middle: DAG canvas area (ReactFlow)
//   3. Bottom/right: detail panel (initially absent)
//
// This test creates its own workflow with stages via API, then navigates
// directly to the overview page.
//
// Depends on: backend workflow + stage API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { loginAsDefault, createTestApi } from "../helpers";
import { test, expect } from "../seed-workflow-overview";
import type { TestApiClient } from "../fixtures";

test.describe("Overview Page Shell Structure", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    slug = await loginAsDefault(page);
    api = await createTestApi();
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("overview page shows three-zone layout with stage cards, DAG, and no detail panel", async ({ page }) => {
    // ── Setup: create a workflow with stages ──
    const workflow = await api.createWorkflow("E2E Shell Test " + Date.now());

    // Create several stages to populate the stage card strip
    await api.createWorkflowStage(workflow.id, "Research", 1);
    await api.createWorkflowStage(workflow.id, "Draft", 2);
    await api.createWorkflowStage(workflow.id, "Review", 3);
    await api.createWorkflowStage(workflow.id, "Finalize", 4);

    // ── Navigate directly to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await page.waitForURL(`/${slug}/workflows/${workflow.id}/overview`);

    // ── Step 1: Verify three main zones ──
    // The overview page should have three zones:
    // - Top: stage card strip (horizontal scrollable area)
    // - Middle: DAG area (main content space)
    // - Right/bottom: no detail panel initially

    // Helper locators for each zone
    const stageCardStrip = page.getByTestId("stage-card-strip").or(
      page.locator('[class*="stage"]').first(),
    );
    const dagArea = page.getByTestId("dag-canvas").or(
      page.locator(".react-flow"),
    );
    const detailPanel = page.getByTestId("detail-panel").or(
      page.locator('[class*="detail-panel"]'),
    );

    // Verify the three zones exist — the strip and DAG are visible,
    // the detail panel is absent or hidden
    await expect(stageCardStrip).toBeVisible({ timeout: 5000 });
    await expect(dagArea).toBeVisible({ timeout: 5000 });
    await expect(detailPanel).not.toBeVisible();

    // ── Step 2: Verify the stage card strip ──
    // The strip should be a horizontally scrollable container with stage cards.
    // Each card should display the stage name.
    const stageCards = stageCardStrip.locator(
      '[class*="stage-card"], [data-testid*="stage-card"]',
    ).or(
      stageCardStrip.getByRole("button"),
    );

    // Expect at least one stage card to be visible
    await expect(stageCards.first()).toBeVisible();
    const cardCount = await stageCards.count();
    expect(cardCount).toBeGreaterThanOrEqual(1);

    // Verify the strip has horizontal overflow (scrollable)
    const hasHorizontalScroll = await stageCardStrip.evaluate((el) => {
      return el.scrollWidth > el.clientWidth;
    });
    // The strip may or may not have overflow depending on viewport width;
    // if all cards fit, it should at least have `overflow-x: auto`.
    // This assertion documents the expected behavior but is soft — it won't
    // fail if the strip isn't scrollable (e.g. few cards in a wide viewport).
    if (cardCount > 2) {
      expect(hasHorizontalScroll).toBe(true);
    }

    // ── Step 3: Verify the DAG area ──
    // The DAG area should contain a ReactFlow canvas with pan/zoom controls.
    // ReactFlow renders a container with class "react-flow" and controls
    // with class "react-flow__controls".
    const reactFlowControls = dagArea.locator(".react-flow__controls").or(
      page.locator(".react-flow__controls"),
    );
    await expect(reactFlowControls).toBeVisible({ timeout: 3000 });

    // The canvas should have the ReactFlow viewport/pane
    const reactFlowPane = dagArea.locator(".react-flow__pane").or(
      page.locator(".react-flow__pane"),
    );
    await expect(reactFlowPane).toBeVisible();

    // ── Step 4: Verify no detail panel is shown initially ──
    // The detail panel is a drawer/sidebar that appears only when a node
    // is selected. On initial page load, it should be absent or hidden.
    await expect(detailPanel).not.toBeVisible();

    // ── Step 5: Page heading ──
    // The page should display the workflow name as a heading
    const heading = page.getByRole("heading", { name: /E2E Shell Test/ }).or(
      page.locator("h1"),
    );
    await expect(heading.first()).toBeVisible();
  });
});
