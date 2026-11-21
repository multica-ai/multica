import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

test("a quiet tool wait is visible and stopping preserves the queued follow-up", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(20_000);
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  let runtimeId: string | undefined;
  let agentId: string | undefined;
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const user = await db.query<{ id: string }>(`SELECT id FROM "user" WHERE email = $1`, [api.getEmail()]);
    const userId = user.rows[0]!.id;
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
       VALUES ($1, 'Hermes verification', 'cloud', 'hermes', 'online', 'E2E fixture', '{}'::jsonb, $2, now()) RETURNING id`,
      [workspace.id, userId],
    );
    runtimeId = runtime.rows[0]!.id;
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Hermes', '', '', 'cloud', '{}'::jsonb, $2, 'private', 'private', 1, $3) RETURNING id`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;
    const issue = await api.createIssue("Review the completed CI job");
    const root = await db.query<{ id: string }>(
      `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
       VALUES ($1, $2, 'member', $3, 'Review the CI result and report back.') RETURNING id`,
      [workspace.id, issue.id, userId],
    );
    const rootId = root.rows[0]!.id;
    const run = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, dispatched_at, started_at, session_id)
       VALUES ($1, $2, $3, $4, ARRAY[$4]::uuid[], 'running', now() - interval '12 minutes', now() - interval '12 minutes', 'ses_quiet') RETURNING id`,
      [agentId, runtimeId, issue.id, rootId],
    );
    const runId = run.rows[0]!.id;
    await db.query(
      `INSERT INTO task_message (task_id, seq, type, tool, call_id, input, created_at)
       VALUES ($1, 1, 'tool_use', 'process_manage', 'wait-1', '{"action":"wait","session_id":"proc_1234"}', now())`,
      [runId],
    );
    const followUp = await db.query<{ id: string }>(
      `INSERT INTO comment (workspace_id, issue_id, parent_id, author_type, author_id, content)
       VALUES ($1, $2, $3, 'member', $4, 'The checks passed. Continue with the review.') RETURNING id`,
      [workspace.id, issue.id, rootId, userId],
    );
    const queued = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status)
       VALUES ($1, $2, $3, $4, 'queued') RETURNING id`,
      [agentId, runtimeId, issue.id, followUp.rows[0]!.id],
    );
    const queuedId = queued.rows[0]!.id;

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${workspace.slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    const block = page.locator(`[data-run-id="${runId}"]`).first();
    await expect(block).toBeVisible({ timeout: 45_000 });
    await expect(block.locator("[data-run-activity-notice]")).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath("recent-activity.png"), animations: "disabled" });

    await db.query(`UPDATE task_message SET created_at = now() - interval '6 minutes' WHERE task_id = $1`, [runId]);
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(block.getByText(/No new events for 6 minutes/)).toBeVisible({ timeout: 30_000 });
    await expect(block.getByText("Waiting for process_manage.")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("quiet-wait.png"), animations: "disabled" });

    await block.getByRole("button", { name: "Open full log" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Waiting for process_manage.")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("quiet-wait-transcript.png"), animations: "disabled" });
    await dialog.getByRole("button", { name: "Close", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await block.getByRole("button", { name: "Open full log" }).click();
    await expect(dialog.getByText("Waiting for process_manage.")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("quiet-wait-mobile.png"), animations: "disabled" });
    await dialog.getByRole("button", { name: "Stop run", exact: true }).click();
    const confirmation = page.getByRole("alertdialog");
    await expect(confirmation).toContainText("Queued comments can continue in a new run.");
    await page.screenshot({ path: testInfo.outputPath("stop-confirmation.png"), animations: "disabled" });
    const cancelled = page.waitForResponse((response) => response.request().method() === "POST" && response.url().includes(`/tasks/${runId}/cancel`));
    await confirmation.getByRole("button", { name: "Stop run", exact: true }).click();
    expect((await cancelled).status()).toBe(200);
    await expect(confirmation).not.toBeVisible();
    await expect(dialog.locator("[data-run-activity-notice]")).toHaveCount(0);

    const tasks = await db.query<{ id: string; status: string; session_id: string }>(
      `SELECT id, status, session_id FROM agent_task_queue WHERE issue_id = $1`, [issue.id],
    );
    expect(tasks.rows).toHaveLength(2);
    expect(tasks.rows.find((row) => row.id === runId)).toMatchObject({ status: "cancelled", session_id: "ses_quiet" });
    expect(tasks.rows.find((row) => row.id === queuedId)).toMatchObject({ status: "queued" });
  } finally {
    await api.cleanup();
    if (agentId) await db.query(`DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)`, [agentId]);
    if (agentId) await db.query(`DELETE FROM agent_task_queue WHERE agent_id = $1`, [agentId]);
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
});
