import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

// Rule semantics live in the Go service and handler tests. This browser test
// covers the sidebar a member uses: the platform's child-done rule is visible
// and can be turned off, and a new wakeup can be created from the form.
test("members see the child-done system rule and create a wakeup", async ({ page }) => {
  test.setTimeout(120_000);
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
       VALUES ($1, 'Wakeup verification', 'cloud', 'e2e_wakeups', 'online', 'E2E fixture', '{}'::jsonb, $2, now()) RETURNING id`,
      [workspace.id, userId],
    );
    runtimeId = runtime.rows[0]!.id;
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Wakeup Emacs', '', '', 'cloud', '{}'::jsonb, $2, 'private', 'private', 1, $3) RETURNING id`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;
    const parent = await api.createIssue("Migrate attachments to multipart upload", { status: "in_progress" });
    // Assign without starting an unrelated assignment run.
    await db.query(`UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2`, [agentId, parent.id]);
    await api.createIssue("Design the multipart API", { parent_issue_id: parent.id, stage: 1, status: "done" });
    await api.createIssue("Measure attachment sizes", { parent_issue_id: parent.id, stage: 1, status: "in_progress" });
    await api.createIssue("Server-side multipart upload", { parent_issue_id: parent.id, stage: 2, status: "backlog" });

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${workspace.slug}/issues/${parent.id}`, { waitUntil: "domcontentloaded" });

    const systemRule = page.getByRole("button", { name: /When stage 1's sub-issues all finish/ });
    await expect(systemRule).toBeVisible({ timeout: 45_000 });
    await expect(systemRule).toContainText("Wake assignee Wakeup Emacs · 1 to go");

    await page.getByRole("button", { name: "New wakeup" }).click();
    const form = page.getByRole("form", { name: "New wakeup" });
    await expect(form).toBeVisible();
    await form.getByRole("button", { name: /Choose a condition/ }).click();
    await page.getByRole("menuitem", { name: /Someone replies/ }).click();
    await form.getByLabel("What to do when woken").fill("Continue stage 2 with the confirmed plan.");
    await form.getByRole("button", { name: /Wait up to/ }).click();
    await page.getByRole("menuitemradio", { name: "3 days" }).click();
    await form.getByRole("button", { name: /^Create/ }).click();
    await expect(form).toBeHidden();

    const created = page.getByRole("button", { name: /When this issue receives a new comment/ });
    await expect(created).toBeVisible();
    await expect(created).toContainText("Wake Wakeup Emacs · Trigger once · 2d 23h left");
    const stored = await db.query<{ expiry_seconds: string; on_timeout: string; created_by: string }>(
      `SELECT expiry_seconds, on_timeout, created_by FROM issue_wakeup WHERE issue_id = $1 AND system_rule IS NULL`,
      [parent.id],
    );
    expect(stored.rows).toEqual([{ expiry_seconds: "259200", on_timeout: "wake", created_by: userId }]);

    await page.getByRole("switch", { name: "Wake the assignee when sub-issues finish" }).first().click();
    await expect(systemRule).toContainText("Turned off for this issue");
    const override = await db.query<{ enabled: boolean; customized: boolean }>(
      `SELECT enabled, customized_at IS NOT NULL AS customized FROM issue_wakeup WHERE issue_id = $1 AND system_rule = 'child_done'`,
      [parent.id],
    );
    expect(override.rows).toEqual([{ enabled: false, customized: true }]);
  } finally {
    await api.cleanup();
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
});

// A condition is checked by the platform: the issue header says what it waits
// for, the scheduler wakes the agent once it holds, and the timeline and the
// workspace list show the rule and its run.
test("a member waits for a condition the platform checks", async ({ page }) => {
  test.setTimeout(150_000);
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
       VALUES ($1, 'Condition verification', 'cloud', 'e2e_wakeups', 'online', 'E2E fixture', '{}'::jsonb, $2, now()) RETURNING id`,
      [workspace.id, userId],
    );
    runtimeId = runtime.rows[0]!.id;
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Condition Grok', '', '', 'cloud', '{}'::jsonb, $2, 'private', 'private', 1, $3) RETURNING id`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;
    const issue = await api.createIssue("Review the upload design", { status: "in_progress" });
    await db.query(`UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2`, [agentId, issue.id]);

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${workspace.slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "New wakeup" }).click({ timeout: 45_000 });
    const form = page.getByRole("form", { name: "New wakeup" });
    await form.getByRole("button", { name: /Choose a condition/ }).click();
    await page.getByRole("menuitem", { name: /A field changes to a value/ }).click();
    await form.getByRole("button", { name: /Choose a status/ }).click();
    await page.getByRole("menuitemradio", { name: "In Review" }).click();
    await form.getByLabel("What to do when woken").fill("Review the design once it is ready.");
    await form.getByRole("button", { name: /^Create/ }).click();
    await expect(form).toBeHidden();

    const rule = page.getByRole("button", { name: /When this issue moves to In Review/ });
    await expect(rule).toBeVisible();
    await expect(page.getByRole("button", { name: /Condition Grok: Waiting for In Review/ })).toBeVisible();
    const stored = await db.query<{ id: string; condition: unknown; event_types: string[] }>(
      `SELECT id, condition, event_types FROM issue_wakeup WHERE issue_id = $1`,
      [issue.id],
    );
    expect(stored.rows[0]).toMatchObject({
      condition: { type: "issue_field", field: "status", value: "in_review" },
      event_types: ["issue.status_changed"],
    });

    // The status change only prompts a check; the scheduler starts the run.
    await api.updateIssue(issue.id, { status: "in_review" });
    await expect
      .poll(
        async () =>
          (await db.query(`SELECT count(*)::int AS n FROM agent_task_queue WHERE context->>'wakeup_id' = $1`, [stored.rows[0]!.id])).rows[0]!.n,
        { timeout: 75_000, intervals: [2_000] },
      )
      .toBe(1);
    await expect(page.getByText("When this issue moves to In Review · woke Condition Grok")).toBeVisible({ timeout: 20_000 });

    await page.goto(`/${workspace.slug}/autopilots?tab=wakeups`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: /^All/ }).click();
    const row = page.getByRole("row", { name: /Review the upload design/ });
    await expect(row).toContainText("When this issue moves to In Review");
    await expect(row).toContainText("1");
    await expect(page.getByRole("columnheader", { name: "Runs (7 days)" })).toBeVisible();
  } finally {
    await api.cleanup();
    if (agentId) {
      await db.query(`DELETE FROM agent_task_queue WHERE agent_id = $1`, [agentId]);
      await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    }
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
});
