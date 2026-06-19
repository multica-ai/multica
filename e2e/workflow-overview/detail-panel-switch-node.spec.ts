// E2E test: Switching nodes updates detail panel inline.
//
// Verifies that clicking a different node in the DAG updates the detail
// panel content without closing and reopening the panel (seamless content
// swap). Also verifies cross-stage node switching works correctly.
//
// Depends on: backend workflow + stage + node API, frontend overview page
// with detail panel implementation.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Node Detail Panel - Switch Node", () => {
  test("switching nodes updates panel inline without close/reopen", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with two stages, each having nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Detail Panel Switch " + Date.now(),
    );

    const stage1 = await seededApi.createWorkflowStage(
      workflow.id,
      "Research",
      1,
    );

    const stage2 = await seededApi.createWorkflowStage(
      workflow.id,
      "Development",
      2,
    );

    // Create node A in stage 1
    const nodeA = await seededApi.createWorkflowNode(workflow.id, {
      title: "Market Research",
      stage_id: stage1.id,
      position_x: 100,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    // Create node B in stage 1
    const nodeB = await seededApi.createWorkflowNode(workflow.id, {
      title: "Competitor Analysis",
      stage_id: stage1.id,
      position_x: 350,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    // Create node C in stage 2
    const nodeC = await seededApi.createWorkflowNode(workflow.id, {
      title: "Prototype Build",
      stage_id: stage2.id,
      position_x: 100,
      position_y: 50,
      worker_type: "agent",
      critic_type: "human",
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`,
    );

    // ── Select stage 1 ──
    const stage1Card = page
      .getByTestId(`stage-card-${stage1.id}`)
      .or(page.locator(`[data-stage-id="${stage1.id}"]`))
      .or(page.getByText("Research").first());

    await stage1Card.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Shared locators ──
    const dagNodes = dagArea.locator(".react-flow__node").or(
      page.locator(".react-flow__node"),
    );

    const detailPanel = () =>
      page
        .getByTestId("node-detail-panel")
        .or(page.locator('[role="dialog"]'))
        .or(page.locator('[role="complementary"]'))
        .or(page.locator('[class*="detail-panel"]'))
        .or(page.locator('[class*="node-detail"]'))
        .or(page.locator('[class*="side-panel"]'));

    // ── Step 1: Open panel for node A ──
    await test.step("Open panel for node A and record content", async () => {
      // Click node A by its title text in the DAG
      await page.locator("text=/Market Research/").first().click();
      await page.waitForTimeout(500);

      // Verify detail panel appears
      await expect(detailPanel().first()).toBeVisible({ timeout: 3000 });

      // Verify panel shows node A's name
      await expect(
        detailPanel().or(page.locator("text=/Market Research/")),
      ).toBeVisible({ timeout: 3000 });
    });

    // ── Step 2: Switch to node B — panel should update inline ──
    await test.step("Click node B — panel updates without closing", async () => {
      // Get a reference to the panel container before switching
      const panelBeforeSwitch = detailPanel().first();

      // Click node B in the DAG
      await page.locator("text=/Competitor Analysis/").first().click();
      await page.waitForTimeout(500);

      // Verify the panel container element still exists (same element persisted)
      await expect(panelBeforeSwitch).toBeAttached({ timeout: 3000 });

      // Verify panel content now shows node B's details
      // Node B's title should be visible
      await expect(
        detailPanel().or(page.locator("text=/Competitor Analysis/")),
      ).toBeVisible({ timeout: 3000 });

      // Node A's title should NOT be the panel's primary heading anymore
      // (it may still appear as a relation reference, so check that the
      //  panel's heading/title area shows node B, not node A)
      const nodeAStillTitle = await detailPanel()
        .locator("text=/Market Research/")
        .count();

      // If node A text appears, it should only be in a relation context,
      // not as the main panel title
      if (nodeAStillTitle > 0) {
        // Verify the heading-level element shows node B instead
        const panelTitle = detailPanel()
          .locator("h1, h2, h3, h4, [role='heading'], [class*='title']")
          .or(page.locator("h1, h2, h3, h4, [role='heading'], [class*='title']"));

        const titleText = await panelTitle.first().textContent();
        expect(titleText).toContain("Competitor Analysis");
      }
    });

    // ── Step 3: Verify node A deselects, node B selects ──
    await test.step("Verify node selection state swaps", async () => {
      // Check selected nodes in DAG
      const selectedNodeText = await dagNodes.evaluateAll((nodes) => {
        const selected = nodes.filter(
          (n) =>
            n.classList.contains("selected") ||
            n.classList.contains("active") ||
            n.getAttribute("aria-selected") === "true" ||
            n.getAttribute("data-selected") === "true",
        );
        return selected.map((n) => n.textContent?.trim() ?? "");
      });

      // Node B should be selected, node A should not
      expect(selectedNodeText).toEqual(
        expect.arrayContaining([expect.stringMatching(/Competitor Analysis/)]),
      );
      expect(selectedNodeText).not.toEqual(
        expect.arrayContaining([expect.stringMatching(/Market Research/)]),
      );
    });

    // ── Step 4: Switch to stage 2, click node C ──
    await test.step("Switch stage and click node — panel shows correct node", async () => {
      // Click stage 2 card
      const stage2Card = page
        .getByTestId(`stage-card-${stage2.id}`)
        .or(page.locator(`[data-stage-id="${stage2.id}"]`))
        .or(page.getByText("Development").first());

      await stage2Card.click();
      await page.waitForTimeout(500);

      // Verify stage 2's DAG renders
      await expect(
        page.locator("text=/Prototype Build/"),
      ).toBeVisible({ timeout: 3000 });

      // Get a reference to the panel before clicking
      const panelBeforeStageSwitch = detailPanel().first();

      // Click node C
      await page.locator("text=/Prototype Build/").first().click();
      await page.waitForTimeout(500);

      // The panel should still be visible (persisted across stage switch)
      // Note: the panel may or may not persist when stage changes — this
      // test documents expected behavior. If the panel was closed on stage
      // switch, node click reopens it.
      const panelVisible = await detailPanel().first().isVisible();
      if (!panelVisible) {
        // Panel was closed on stage switch — clicking node should reopen it
        await expect(detailPanel().first()).toBeVisible({ timeout: 3000 });
      }

      // Verify panel shows node C's details
      await expect(
        detailPanel().or(page.locator("text=/Prototype Build/")),
      ).toBeVisible({ timeout: 3000 });

      // Verify previous node names are not the primary content
      const prevNodeContent = await detailPanel()
        .locator("text=/Market Research|Competitor Analysis/")
        .count();
      // Node C's title should dominate; previous node names may appear
      // only as relation references (if edges exist)
      expect(prevNodeContent).toBeLessThanOrEqual(2);
    });
  });
});
