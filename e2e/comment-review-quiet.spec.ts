import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

test("plain review and done comments stay quiet in the composer and after posting", async ({ page }) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  let runtimeId: string | undefined;
  let agentId: string | undefined;
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const user = await db.query<{ id: string }>(`SELECT id FROM "user" WHERE email = $1`, [api.getEmail()]);
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
       VALUES ($1, 'Quiet review fake', 'cloud', 'e2e_quiet_review', 'online', 'E2E fixture', '{}'::jsonb, $2, now()) RETURNING id`,
      [workspace.id, user.rows[0]!.id],
    );
    runtimeId = runtime.rows[0]!.id;
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Quiet review assignee', '', '', 'cloud', '{}'::jsonb, $2, 'private', 'private', 1, $3) RETURNING id`,
      [workspace.id, runtimeId, user.rows[0]!.id],
    );
    agentId = agent.rows[0]!.id;
    const issue = await api.createIssue("Review comments must not start an agent");
    await db.query(`UPDATE issue SET assignee_type = 'agent', assignee_id = $1, status = 'in_review' WHERE id = $2`, [agentId, issue.id]);
    await page.addInitScript((token) => { localStorage.setItem("multica_token", token!); localStorage.setItem("multica:chat:isOpen", "false"); }, api.getToken());
    await page.goto(`/${workspace.slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });

    for (const [status, content] of [["in_review", "Accepted after review"], ["done", "Final status recorded"]] as const) {
      if (status === "done") {
        await db.query(`UPDATE issue SET status = 'done' WHERE id = $1`, [issue.id]);
        await page.reload({ waitUntil: "domcontentloaded" });
      }
      await page.getByTestId("comment-composer-shell").click();
      const preview = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/api/issues/${issue.id}/comments/trigger-preview`));
      await page.locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])').first().fill(content);
      const previewResponse = await preview;
      expect(previewResponse.status()).toBe(200);
      expect((await previewResponse.json()).agents).toHaveLength(0);
      const posted = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/api/issues/${issue.id}/comments`));
      await page.keyboard.press("ControlOrMeta+Enter");
      const response = await posted;
      expect(response.status()).toBe(201);
      const comment = await response.json();
      const runs = await db.query(`SELECT id FROM agent_task_queue WHERE trigger_comment_id = $1`, [comment.id]);
      expect(runs.rows).toHaveLength(0);
      await page.reload({ waitUntil: "domcontentloaded" });
      await expect(page.getByText(content, { exact: true })).toBeVisible();
    }
  } finally {
    await api.cleanup();
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
});
