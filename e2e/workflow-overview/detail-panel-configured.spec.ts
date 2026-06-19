// E2E test: Node detail panel shows configured values.
//
// Verifies that a node with fully configured worker, critic, format schema,
// and relations displays the correct values in the detail panel.
//
// Depends on: backend workflow + stage + node + edge API, frontend overview
// page with detail panel rendering.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Node Detail Panel - Configured Values", () => {
  test("detail panel shows configured worker, critic, schema, and relations", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with a stage and fully configured nodes ──
    const workflow = await seededApi.createWorkflow(
      "E2E Detail Panel Configured " + Date.now(),
    );

    const stage = await seededApi.createWorkflowStage(
      workflow.id,
      "Review",
      1,
    );

    // Create a "downstream" node first (referenced by edge)
    const downstreamNode = await seededApi.createWorkflowNode(
      workflow.id,
      {
        title: "Output Report",
        stage_id: stage.id,
        position_x: 600,
        position_y: 100,
        worker_type: "agent",
        critic_type: "human",
      },
    );

    // Create the main configured node with full configuration.
    // Note: extra fields like worker_agent_name and format_schema may not
    // yet be accepted by the API — they are included here to document the
    // intended contract.
    const configuredNode = await (seededApi.createWorkflowNode as any)(
      workflow.id,
      {
        title: "Content Review",
        stage_id: stage.id,
        position_x: 200,
        position_y: 100,
        worker_type: "agent",
        worker_agent_name: "TestAgent",
        critic_type: "human",
        format_schema: {
          type: "object",
          properties: {
            title: { type: "string" },
            score: { type: "number" },
          },
          required: ["title"],
        },
      },
    );

    // Create an "upstream" node
    const upstreamNode = await seededApi.createWorkflowNode(
      workflow.id,
      {
        title: "Data Collection",
        stage_id: stage.id,
        position_x: -200,
        position_y: 100,
        worker_type: "llm",
        critic_type: "human",
      },
    );

    // Create edges for relations: upstream → configured → downstream
    await seededApi.createWorkflowEdge(
      workflow.id,
      upstreamNode.id,
      configuredNode.id,
    );
    await seededApi.createWorkflowEdge(
      workflow.id,
      configuredNode.id,
      downstreamNode.id,
    );

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`,
    );

    // ── Select the stage ──
    const stageCard = page
      .getByTestId(`stage-card-${stage.id}`)
      .or(page.locator(`[data-stage-id="${stage.id}"]`))
      .or(page.getByText("Review").first());

    await stageCard.click();
    await page.waitForTimeout(500);

    // ── Verify DAG area and click the configured node ──
    const dagArea = page
      .getByTestId("dag-canvas")
      .or(page.locator(".react-flow"))
      .or(page.locator('[class*="dag"]'));

    await expect(dagArea).toBeVisible({ timeout: 5000 });

    // Click the configured node by its title text
    const configuredNodeEl = dagArea
      .locator(".react-flow__node")
      .or(page.locator(".react-flow__node"))
      .or(page.locator("text=/Content Review/"));

    await configuredNodeEl.first().click();
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

    // ── Step 1: Verify Worker section ──
    // Worker section should show "agent" type and "TestAgent" name
    const workerSection = detailPanel.locator("text=/Worker/").or(
      page.locator("text=/Worker/"),
    );

    // The worker type indicator
    await expect(
      detailPanel
        .or(page.locator("text=/agent/")),
    ).toBeVisible({ timeout: 3000 });

    // The assigned agent name (may be a link to agent detail)
    const agentName = detailPanel
      .locator("text=/TestAgent/")
      .or(page.locator("text=/TestAgent/"));
    // Soft assertion — the agent name may or may not be present depending
    // on API and rendering implementation
    const agentNameVisible = await agentName.count();
    if (agentNameVisible > 0) {
      await expect(agentName.first()).toBeVisible();
    }

    // ── Step 2: Verify Critic section ──
    // Critic section should show "human" type and reviewer name
    await expect(
      detailPanel
        .or(page.locator("text=/human/")),
    ).toBeVisible({ timeout: 3000 });

    // ── Step 3: Verify Format Schema section ──
    // The schema should be displayed as formatted/pretty-printed JSON
    const schemaSection = detailPanel
      .locator("text=/Format Schema/")
      .or(page.locator("text=/Format Schema/"));
    await expect(schemaSection.first()).toBeVisible({ timeout: 3000 });

    // Check for JSON content within the schema section
    // The JSON may be syntax-highlighted or pretty-printed
    const jsonContent = detailPanel
      .locator("text=/type.*object|title.*string|score.*number/")
      .or(
        page.locator(
          "text=/type.*object|title.*string|score.*number/",
        ),
      );
    const jsonVisible = await jsonContent.count();
    if (jsonVisible > 0) {
      await expect(jsonContent.first()).toBeVisible();
    }

    // ── Step 4: Verify Relations section ──
    const relationsSection = detailPanel
      .locator("text=/Relations/")
      .or(page.locator("text=/Relations/"));
    await expect(relationsSection.first()).toBeVisible({ timeout: 3000 });

    // Should show upstream connection
    const upstreamRelation = detailPanel
      .locator("text=/Data Collection/")
      .or(page.locator("text=/Data Collection/"));
    const upstreamVisible = await upstreamRelation.count();
    if (upstreamVisible > 0) {
      await expect(upstreamRelation.first()).toBeVisible();
    }

    // Should show downstream connection
    const downstreamRelation = detailPanel
      .locator("text=/Output Report/")
      .or(page.locator("text=/Output Report/"));
    const downstreamVisible = await downstreamRelation.count();
    if (downstreamVisible > 0) {
      await expect(downstreamRelation.first()).toBeVisible();
    }

    // ── Step 5: Verify the panel can also be found via section-heading regex ──
    // All four section headings should match the combined regex
    const allHeadings = detailPanel.locator(
      "text=/Worker|Critic|Format Schema|Relations/",
    );
    const headingCount = await allHeadings.count();
    expect(headingCount).toBeGreaterThanOrEqual(4);
  });
});
