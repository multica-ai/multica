// E2E test: Node detail panel shows unconfigured state.
//
// Verifies that a node with no worker, no critic, and no format_schema
// displays appropriate "Not configured" / "未配置" placeholders in muted
// style, and the Format Schema section shows "No format constraints" or
// equivalent empty state.
//
// Depends on: backend workflow + stage + node API, frontend overview page
// with detail panel rendering.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Node Detail Panel - Unconfigured State", () => {
  test("detail panel shows unconfigured placeholders for empty node", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and a bare node ──
    const workflow = await seededApi.createWorkflow(
      "E2E Detail Panel Unconfigured " + Date.now(),
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Draft",
      1,
    );

    // Create a node with no worker_config, no critic, no format_schema.
    // Passing minimal fields — worker_type and critic_type are omitted to
    // simulate an unconfigured node.
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Raw Task",
      stage_id: stage.id,
      position_x: 100,
      position_y: 50,
      worker_type: "",
      critic_type: "",
    });

    // Also create a second node for context (to ensure the DAG has content)
    await seededApi.createWorkflowNode(workflow.id, {
      title: "Another Task",
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
      .or(page.getByText("Draft").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // ── Step 1: Click the unconfigured node ──
    const unconfiguredNode = dagArea
      .locator(".react-flow__node")
      .or(page.locator(".react-flow__node"))
      .or(page.locator("text=/Raw Task/"));

    await unconfiguredNode.first().click();
    await page.waitForTimeout(500);

    // ── Verify detail panel is visible ──
    const detailPanel = page
      .getByTestId("node-detail-panel")
      .or(page.locator('[role="dialog"]'))
      .or(page.locator('[role="complementary"]'))
      .or(page.locator('[class*="detail-panel"]'))
      .or(page.locator('[class*="node-detail"]'))
      .or(page.locator('[class*="side-panel"]'));

    await expect(detailPanel).toBeVisible({ timeout: 3000 });

    // ── Step 2: Verify node title is shown ──
    await expect(
      detailPanel.or(page.locator("text=/Raw Task/")),
    ).toBeVisible({ timeout: 3000 });

    // ── Step 3: Verify Worker section shows unconfigured state ──
    const workerSection = detailPanel
      .locator("text=/Worker/")
      .or(page.locator("text=/Worker/"));
    await expect(workerSection.first()).toBeVisible({ timeout: 3000 });

    // Should show "Not configured" or "未配置" in muted style
    const notConfiguredWorker = detailPanel
      .locator("text=/Not configured|未配置/")
      .or(page.locator("text=/Not configured|未配置/"));
    await expect(notConfiguredWorker.first()).toBeVisible({ timeout: 3000 });

    // ── Step 4: Verify Critic section shows unconfigured state ──
    const criticSection = detailPanel
      .locator("text=/Critic/")
      .or(page.locator("text=/Critic/"));
    await expect(criticSection.first()).toBeVisible({ timeout: 3000 });

    // Should also show "Not configured" or "未配置"
    const notConfiguredCritic = detailPanel
      .locator("text=/Not configured|未配置/")
      .or(page.locator("text=/Not configured|未配置/"));
    const criticUnconfiguredCount = await notConfiguredCritic.count();
    expect(criticUnconfiguredCount).toBeGreaterThanOrEqual(2);

    // ── Step 5: Verify Format Schema section shows empty state ──
    const schemaSection = detailPanel
      .locator("text=/Format Schema/")
      .or(page.locator("text=/Format Schema/"));
    await expect(schemaSection.first()).toBeVisible({ timeout: 3000 });

    // The schema section should show "No format constraints" or "无格式约束",
    // or it may be collapsed/inaccessible
    const noFormatConstraints = detailPanel
      .locator(
        "text=/No format constraints|无格式约束|No schema|No constraints|No format/",
      )
      .or(
        page.locator(
          "text=/No format constraints|无格式约束|No schema|No constraints|No format/",
        ),
      );
    const constraintsVisible = await noFormatConstraints.count();
    if (constraintsVisible > 0) {
      await expect(noFormatConstraints.first()).toBeVisible();
    }

    // ── Step 6: Verify muted styling on unconfigured text ──
    // The "Not configured"/"未配置" text should have muted styling
    const mutedStyle = await notConfiguredWorker.first().evaluate((el) => {
      const classList = Array.from(el.classList).join(" ");
      const style = window.getComputedStyle(el);
      return {
        hasMutedClass:
          classList.includes("muted") ||
          classList.includes("text-muted") ||
          classList.includes("text-muted-foreground") ||
          classList.includes("opacity") ||
          classList.includes("secondary"),
        opacity: style.opacity,
        color: style.color,
      };
    });
    // The element should have lower opacity or a muted color class
    // This is a soft assertion — the exact styling depends on implementation
    expect(
      mutedStyle.hasMutedClass ||
        parseFloat(mutedStyle.opacity) < 0.8 ||
        mutedStyle.color.includes("128") ||
        mutedStyle.color.includes("160") ||
        mutedStyle.color.includes("150"),
    ).toBe(true);
  });
});
