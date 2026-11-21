import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

// Routing matrices live in the Go handler tests. This browser regression covers
// the reply composer, real API routing, and historical inline-run visibility.
test("reply stays with the thread agent and hides unused historical fallbacks", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  let runtimeId: string | undefined;
  const agentIds: string[] = [];
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const user = await db.query<{ id: string }>(`SELECT id FROM "user" WHERE email = $1`, [api.getEmail()]);
    const userId = user.rows[0]!.id;
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
       VALUES ($1, 'Comment routing verification', 'cloud', 'e2e_comment_routing', 'online', 'E2E fixture', '{}'::jsonb, $2, now()) RETURNING id`,
      [workspace.id, userId],
    );
    runtimeId = runtime.rows[0]!.id;
    for (const name of ["Agent A Assignee", "Agent B Thread Owner", "Agent C Cancelled Control"]) {
      const row = await db.query<{ id: string }>(
        `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
         VALUES ($1, $2, '', '', 'cloud', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id`,
        [workspace.id, name, runtimeId, userId],
      );
      agentIds.push(row.rows[0]!.id);
    }
    const [agentA, agentB, agentC] = agentIds;
    const issue = await api.createIssue("Verify reply routing and historical cancelled blocks");
    // Seed assignment without triggering an unrelated assignment run.
    await db.query(`UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2`, [agentA, issue.id]);
    const root = await db.query<{ id: string }>(
      `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
       VALUES ($1, $2, 'member', $3, $4) RETURNING id`,
      [workspace.id, issue.id, userId, `[@Agent B Thread Owner](mention://agent/${agentB}) Please investigate this thread.`],
    );
    const rootId = root.rows[0]!.id;
    const primary = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, started_at, completed_at)
       VALUES ($1, $2, $3, $4, ARRAY[$4]::uuid[], 'completed', now() - interval '1 minute', now()) RETURNING id`,
      [agentB, runtimeId, issue.id, rootId],
    );
    const primaryId = primary.rows[0]!.id;
    await db.query(
      `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, parent_id, source_task_id)
       VALUES ($1, $2, 'agent', $3, 'Agent B is following this thread. Please reply here.', $4, $5)`,
      [workspace.id, issue.id, agentB, rootId, primaryId],
    );
    const hiddenIds: string[] = [];
    for (const dispatched of [false, true]) {
      const row = await db.query<{ id: string }>(
        `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, escalation_for_task_id, dispatched_at, completed_at)
         VALUES ($1, $2, $3, $4, 'cancelled', $5, CASE WHEN $6 THEN now() ELSE NULL END, now()) RETURNING id`,
        [agentA, runtimeId, issue.id, rootId, primaryId, dispatched],
      );
      hiddenIds.push(row.rows[0]!.id);
    }
    const control = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, completed_at)
       VALUES ($1, $2, $3, $4, 'cancelled', now()) RETURNING id`,
      [agentC, runtimeId, issue.id, rootId],
    );
    const controlId = control.rows[0]!.id;
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1050 });
    await page.goto(`/${workspace.slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText("Agent B is following this thread. Please reply here.", { exact: true })).toBeVisible({ timeout: 45_000 });
    await expect(page.locator(`[data-run-id="${controlId}"]`)).toBeVisible();
    for (const id of hiddenIds) await expect(page.locator(`[data-run-id="${id}"]`)).toHaveCount(0);

    await page.getByTestId("reply-composer-shell").first().click();
    const editor = page.locator('.ProseMirror[data-placeholder="Leave a reply..."], .ProseMirror:has([data-placeholder="Leave a reply..."])').first();
    await editor.fill("Please continue, Agent B. The issue remains assigned to Agent A.");
    const posted = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/api/issues/${issue.id}/comments`));
    await page.keyboard.press("ControlOrMeta+Enter");
    const response = await posted;
    expect(response.status()).toBe(201);
    const reply = await response.json();
    const tasks = await db.query<{ id: string; agent_id: string; status: string; escalation_for_task_id: string | null }>(
      `SELECT id, agent_id, status, escalation_for_task_id FROM agent_task_queue WHERE trigger_comment_id = $1`, [reply.id],
    );
    expect(tasks.rows).toHaveLength(1);
    expect(tasks.rows[0]).toMatchObject({ agent_id: agentB, status: "queued", escalation_for_task_id: null });
    await expect(page.locator(`[data-run-id="${tasks.rows[0]!.id}"]`)).toBeVisible();
    for (const id of hiddenIds) await expect(page.locator(`[data-run-id="${id}"]`)).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath("thread-reply.png"), fullPage: true });

    // Reload proves persisted history is filtered, not just the live update.
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator(`[data-run-id="${tasks.rows[0]!.id}"]`)).toBeVisible({ timeout: 30_000 });
    await expect(page.locator(`[data-run-id="${controlId}"]`)).toBeVisible();
    for (const id of hiddenIds) await expect(page.locator(`[data-run-id="${id}"]`)).toHaveCount(0);

    await page.getByTestId("comment-composer-shell").click();
    await page.locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])').first().fill("A separate top-level request for the assignee.");
    const topLevelPosted = page.waitForResponse((res) => res.request().method() === "POST" && res.url().endsWith(`/api/issues/${issue.id}/comments`));
    await page.keyboard.press("ControlOrMeta+Enter");
    const topLevelResponse = await topLevelPosted;
    expect(topLevelResponse.status()).toBe(201);
    const topLevel = await topLevelResponse.json();
    const topTasks = await db.query<{ id: string; agent_id: string }>(`SELECT id, agent_id FROM agent_task_queue WHERE trigger_comment_id = $1`, [topLevel.id]);
    expect(topTasks.rows).toHaveLength(1);
    expect(topTasks.rows[0]!.agent_id).toBe(agentA);
    await expect(page.locator(`[data-run-id="${topTasks.rows[0]!.id}"]`)).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("top-level-assignee.png"), fullPage: true });
  } finally {
    await api.cleanup();
    if (agentIds.length) await db.query(`DELETE FROM agent WHERE id = ANY($1::uuid[])`, [agentIds]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
});
