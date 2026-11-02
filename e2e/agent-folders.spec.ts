import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// OL-1170: in-place multi-level folders, aggregate presence, composite keys,
// subfolder creation and workspace-scoped expansion persistence.

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const SHOT_DIR = process.env.AGENT_FOLDERS_SCREENSHOT_DIR;
const AGENTS = ["Alpha", "Beta", "Gamma"] as const;

test("agents page folder explorer: in-place tree, presence, and placements", async ({ page }) => {
  test.setTimeout(120000);
  const api: TestApiClient = await createTestApi();
  const db = new pg.Client(DATABASE_URL);
  await db.connect();

  let runtimeId: string | null = null;
  const agentIds: string[] = [];
  let squadId: string | null = null;

  try {
    const workspace = (await api.getWorkspaces())[0];
    if (!workspace) throw new Error("E2E workspace missing");
    api.setWorkspaceId(workspace.id);
    api.setWorkspaceSlug(workspace.slug);

    const user = await db.query<{ id: string }>(
      `SELECT id::text FROM "user" WHERE email = $1 LIMIT 1`,
      [api.getEmail()],
    );
    const userId = user.rows[0]?.id;
    if (!userId) throw new Error("E2E user missing");

    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', 'e2e_agent_folders', 'online', $3, '{}'::jsonb, now())
       RETURNING id::text`,
      [workspace.id, `Agent folders ${Date.now()}`, "Agent folders E2E"],
    );
    runtimeId = runtime.rows[0]!.id;

    for (const name of AGENTS) {
      const agent = await db.query<{ id: string }>(
        `INSERT INTO agent (
           workspace_id, name, description, instructions, runtime_mode,
           runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id
         )
         VALUES ($1, $2, '', '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
         RETURNING id::text`,
        [workspace.id, name, runtimeId, userId],
      );
      agentIds.push(agent.rows[0]!.id);
    }
    const [alphaId, betaId, gammaId] = agentIds as [string, string, string];
    await db.query(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
       VALUES ($1, $2, 'running', 0)`,
      [alphaId, runtimeId],
    );
    await api.postJson("/api/agent-folders", { name: "Research", parent_id: "" });

    const token = api.getToken();
    if (!token) throw new Error("E2E token missing");
    await page.addInitScript((authToken) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);
    await page.setViewportSize({ width: 1600, height: 1000 });
    await page.goto(`/${workspace.slug}/agents`, { waitUntil: "domcontentloaded" });

    await expect(page.getByRole("button", { name: "Expand Research" })).toBeVisible({ timeout: 30000 });
    for (const name of AGENTS) {
      await expect(page.locator('[role="row"]', { hasText: name }).first()).toBeVisible();
    }

    await page.getByRole("button", { name: "New folder" }).click();
    await page.getByLabel("Name").fill("Design");
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByRole("button", { name: "Expand Design" })).toBeVisible();

    const alphaRow = page.locator('[role="row"]', { hasText: "Alpha" });
    await alphaRow.getByRole("button", { name: "Row actions" }).click();
    await page.getByRole("menuitem", { name: "Move to..." }).click();
    await page.getByRole("button", { name: "Design", exact: true }).click();
    await page.getByRole("button", { name: "Move", exact: true }).click();
    await expect(page.locator('[role="row"]', { hasText: "Alpha" })).toHaveCount(0);

    const squad = await api.postJson<{ id: string }>("/api/squads", {
      name: "Game design",
      leader_id: betaId,
    });
    squadId = squad.id;
    await api.postJson(`/api/squads/${squadId}/members`, {
      member_type: "agent",
      member_id: gammaId,
      role: "member",
    });
    await expect(page.getByRole("button", { name: "Expand Game design" })).toBeVisible({ timeout: 30000 });

    await page.getByRole("button", { name: "Actions for Game design" }).click();
    await page.getByRole("menuitem", { name: "Move to..." }).click();
    await page.getByRole("button", { name: "Design", exact: true }).click();
    await page.getByRole("button", { name: "Move", exact: true }).click();
    await expect(page.getByRole("button", { name: "Expand Game design" })).toHaveCount(0);

    await page.getByRole("button", { name: "Actions for Design" }).click();
    await page.getByRole("menuitem", { name: "New subfolder" }).click();
    await page.getByLabel("Name").fill("Nested");
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByRole("button", { name: "Collapse Design" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Expand Nested" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Expand Game design" })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: "Alpha" })).toBeVisible();

    await page.getByRole("button", { name: "Expand Game design" }).click();
    await expect(page.locator('[role="row"]', { hasText: "Beta" })).toBeVisible();
    const gammaRow = page.locator('[role="row"]', { hasText: "Gamma" });
    await expect(gammaRow).toBeVisible();

    const designRow = page.locator('[role="row"]').filter({
      has: page.getByRole("button", { name: "Collapse Design", exact: true }),
    });
    await expect(designRow).toContainText("1 running");
    await expect(designRow).toContainText("3 online");
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/01-in-place-status.png`, fullPage: true });

    await gammaRow.getByRole("button", { name: "Row actions" }).click();
    await page.getByRole("menuitem", { name: "Also place in folder..." }).click();
    await page.getByRole("button", { name: "Research", exact: true }).click();
    await page.getByRole("button", { name: "Place", exact: true }).click();
    await page.getByRole("button", { name: "Expand Research" }).click();
    await expect(page.locator('[role="row"]', { hasText: "Gamma" })).toHaveCount(2);
    expect(page.url()).not.toContain("folder=");
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/02-multi-location.png`, fullPage: true });

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("button", { name: "Collapse Design" })).toBeVisible({ timeout: 30000 });
    await expect(page.getByRole("button", { name: "Collapse Game design" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Collapse Research" })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: "Gamma" })).toHaveCount(2);

    await page.getByRole("button", { name: "Collapse all" }).click();
    for (const name of AGENTS) {
      await expect(page.locator('[role="row"]', { hasText: name })).toHaveCount(0);
    }
    await page.getByRole("button", { name: "Expand all" }).click();
    await expect(page.locator('[role="row"]', { hasText: "Gamma" })).toHaveCount(2);

    const search = page.getByRole("textbox", { name: "Search agents…" });
    await search.fill("Gamma");
    await expect(page.getByText("Design / Game design", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "+1", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Collapse Design" })).toHaveCount(0);
    await search.fill("");
    await expect(page.getByRole("button", { name: "Collapse Design" })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: "Gamma" })).toHaveCount(2);
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/03-reload-persisted.png`, fullPage: true });

    await page.setViewportSize({ width: 1366, height: 900 });
    const designNameBox = await designRow.getByText("Design", { exact: true }).boundingBox();
    if (!designNameBox) throw new Error("Design folder name is not visible at 1366px");
    expect(designNameBox.width).toBeGreaterThan(40);
    const squadNameBox = await page.getByText("Game design", { exact: true }).boundingBox();
    if (!squadNameBox) throw new Error("Squad folder name is not visible at 1366px");
    expect(squadNameBox.width).toBeGreaterThan(40);
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/04-laptop-1366.png`, fullPage: true });

    await page.getByRole("button", { name: "Last active", exact: true }).first().click();
    await page.getByRole("switch", { name: "Model", exact: true }).click();
    await page.getByRole("switch", { name: "Created", exact: true }).click();
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 1440, height: 900 });
    const allColumnsNameBox = await page.getByText("Game design", { exact: true }).boundingBox();
    if (!allColumnsNameBox) throw new Error("Squad folder name is not visible with all columns at 1440px");
    expect(allColumnsNameBox.width).toBeGreaterThan(60);
    const nameCellBox = await designRow
      .getByRole("button", { name: "Collapse Design", exact: true })
      .locator("..")
      .boundingBox();
    if (!nameCellBox) throw new Error("Folder name track missing with all columns");
    const countLabel = designRow.getByText("3 agents", { exact: true });
    const squadLabel = page.getByText("Squad", { exact: true });
    if (nameCellBox.width <= 22 * 16) {
      await expect(countLabel).toBeHidden();
      await expect(squadLabel).toBeHidden();
    } else {
      await expect(countLabel).toBeVisible();
      await expect(squadLabel).toBeVisible();
    }
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/05-all-columns-1440.png`, fullPage: true });

    await page.setViewportSize({ width: 600, height: 900 });
    const alphaRowForLayout = page.locator('[role="row"]', { hasText: "Alpha" });
    const visibleCellCount = (row: typeof designRow) => row.evaluate((element) =>
      Array.from(element.querySelectorAll<HTMLElement>('[role="cell"]'))
        .filter((cell) => getComputedStyle(cell).display !== "none")
        .length,
    );
    expect(await visibleCellCount(designRow)).toBe(await visibleCellCount(alphaRowForLayout));
    const folderStatusBox = await designRow.getByRole("cell").filter({ hasText: "1 running" }).boundingBox();
    const agentStatusBox = await alphaRowForLayout.getByRole("cell").filter({ hasText: "Online" }).boundingBox();
    if (!folderStatusBox || !agentStatusBox) throw new Error("Status cells missing at 600px");
    expect(Math.abs(folderStatusBox.x - agentStatusBox.x)).toBeLessThanOrEqual(1);
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/06-narrow-600.png`, fullPage: true });
  } finally {
    if (squadId) {
      await db.query(`DELETE FROM squad_member WHERE squad_id = $1`, [squadId]);
      await db.query(`DELETE FROM squad WHERE id = $1`, [squadId]);
    }
    await db.query(`DELETE FROM agent_task_queue WHERE agent_id = ANY($1::uuid[])`, [agentIds]);
    await db.query(
      `DELETE FROM labrastro_agent_placement WHERE workspace_id = (SELECT workspace_id FROM agent_runtime WHERE id = $1)`,
      [runtimeId],
    );
    await db.query(
      `DELETE FROM labrastro_agent_squad_placement WHERE workspace_id = (SELECT workspace_id FROM agent_runtime WHERE id = $1)`,
      [runtimeId],
    );
    await db.query(
      `DELETE FROM labrastro_agent_folder WHERE workspace_id = (SELECT workspace_id FROM agent_runtime WHERE id = $1)`,
      [runtimeId],
    );
    for (const id of agentIds) {
      await db.query(`DELETE FROM agent WHERE id = $1`, [id]);
    }
    if (runtimeId) {
      await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    }
    await db.end();
    await api.cleanup();
  }
});
