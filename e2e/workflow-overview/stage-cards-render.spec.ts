// E2E test: Stage cards render with correct data.
//
// Seeds a workflow with 3 stages ("需求" 2 nodes, "设计" 3 nodes, "编码" 1 node)
// and verifies card count, names, node counts, and sort_order ordering.
//
// Depends on: backend workflow + stage + node API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Stage Cards Rendering", () => {
  test("stage cards render correct names, node counts, and ordering", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages and nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Stage Cards Test " + Date.now()
    );

    // Create 3 stages with different node counts per the spec
    const stage1 = await seededApi.createWorkflowStage(workflow.id, "需求", 1);
    const stage2 = await seededApi.createWorkflowStage(workflow.id, "设计", 2);
    const stage3 = await seededApi.createWorkflowStage(workflow.id, "编码", 3);

    // Assign nodes to each stage
    // Stage "需求" gets 2 nodes
    await seededApi.createWorkflowNode(workflow.id, {
      title: "需求收集",
      stage_id: stage1.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "需求分析",
      stage_id: stage1.id,
    });

    // Stage "设计" gets 3 nodes
    await seededApi.createWorkflowNode(workflow.id, {
      title: "UI设计",
      stage_id: stage2.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "交互设计",
      stage_id: stage2.id,
    });
    await seededApi.createWorkflowNode(workflow.id, {
      title: "视觉设计",
      stage_id: stage2.id,
    });

    // Stage "编码" gets 1 node
    await seededApi.createWorkflowNode(workflow.id, {
      title: "前端开发",
      stage_id: stage3.id,
    });

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 1: Verify exactly 3 stage cards visible ──
    const stageCards = page
      .getByTestId("stage-card-strip")
      .locator('[class*="stage-card"], [data-testid*="stage-card"]')
      .or(page.getByTestId("stage-card-strip").getByRole("listitem"));

    // Fallback: look for cards by container testid matching
    const allStageCards = page
      .getByTestId(/^stage-card-/)
      .or(stageCards);

    await expect(allStageCards.first()).toBeVisible({ timeout: 5000 });
    const cardCount = await allStageCards.count();
    expect(cardCount).toBe(3);

    // ── Step 2: Verify first card shows "需求" and node count ──
    const firstCard = allStageCards.first();
    await expect(firstCard).toContainText(/需求/);
    await expect(firstCard).toContainText(/\d+ nodes?|\d+ 个节点/);

    // ── Step 3: Verify cards are ordered by sort_order ──
    // The cards should appear in the DOM in the order: 需求 (sort_order 1),
    // 设计 (sort_order 2), 编码 (sort_order 3).
    const cardTexts = await allStageCards.evaluateAll((cards) =>
      cards.map((card) => card.textContent ?? "")
    );

    // Extract stage names in DOM order
    const stageNames = cardTexts.map((text) => {
      if (text.includes("需求")) return "需求";
      if (text.includes("设计")) return "设计";
      if (text.includes("编码")) return "编码";
      return text;
    });

    expect(stageNames[0]).toBe("需求");
    expect(stageNames[1]).toBe("设计");
    expect(stageNames[2]).toBe("编码");

    // ── Step 4: Verify node counts per spec ──
    // "需求" has 2 nodes, "设计" has 3 nodes, "编码" has 1 node
    // Extract the node counts from each card
    const nodeCountTexts = await allStageCards.evaluateAll((cards) =>
      cards.map((card) => {
        const text = card.textContent ?? "";
        const match = text.match(/(\d+)\s*(node|个)/i);
        return match ? parseInt(match[1], 10) : null;
      })
    );

    // 需求 should have 2 nodes (earliest in DOM because sort_order=1)
    // Note: We can't be certain which card is which by position alone,
    // so we match by name
    const demandCardIndex = stageNames.indexOf("需求");
    const designCardIndex = stageNames.indexOf("设计");
    const codeCardIndex = stageNames.indexOf("编码");

    expect(nodeCountTexts[demandCardIndex]).toBe(2);
    expect(nodeCountTexts[designCardIndex]).toBe(3);
    expect(nodeCountTexts[codeCardIndex]).toBe(1);
  });
});
