import "./env";
import { expect, type Page, test } from "@playwright/test";
import pg from "pg";

import { createTestApi } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

async function setUserDecision(
  database: pg.Client,
  workspaceId: string,
  userId: string,
  toolKey: "create_autopilot" | "trigger_autopilot",
  setting: "allow" | "deny",
) {
  await database.query(
    `INSERT INTO cerebro_tool_policy
       (workspace_id, tool_key, layer, subject_id, resource_pattern, setting)
     VALUES ($1, $2, 'user', $3, '', $4)
     ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern)
     DO UPDATE SET setting = EXCLUDED.setting, updated_at = NOW()`,
    [workspaceId, toolKey, userId, setting],
  );
}

async function openAsMember(
  page: Page,
  token: string,
  userId: string,
  workspaceSlug: string,
  path: string,
) {
  await page.addInitScript(
    ({ authToken, attributionKey }) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem(attributionKey, "3");
    },
    { authToken: token, attributionKey: `multica.source_backfill.dismiss.${userId}` },
  );
  await page.goto(`/${workspaceSlug}${path}`, { waitUntil: "domcontentloaded" });
}

test("an ordinary member controls an autopilot through the existing Permissions decisions", async ({ page }) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  const workspaceSlug = api.getWorkspaceSlug();
  if (!workspaceId || !workspaceSlug) throw new Error("E2E workspace was not resolved");

  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  let memberUserId = "";
  let runtimeId = "";
  let agentId = "";
  let autopilotId = "";
  let privateAutopilotId = "";

  try {
    await api.setWorkspaceFeatureFlag("cerebro_tool_policy", true);
    await api.setWorkspaceFeatureFlag("cerebro_member_override", true);
    const member = await api.loginSecondaryUser(
      `fir-4359-${suffix}@multica.ai`,
      `FIR 4359 Member ${suffix}`,
    );
    memberUserId = member.userId;
    const ownerUserId = (await database.query(
      `SELECT user_id FROM member WHERE workspace_id = $1 AND role = 'owner' LIMIT 1`,
      [workspaceId],
    )).rows[0].user_id as string;
    runtimeId = (await database.query(
      `INSERT INTO agent_runtime
         (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
       VALUES ($1, $2, 'cloud', 'e2e_runtime', 'online', $2, '{}'::jsonb, NOW()) RETURNING id`,
      [workspaceId, `FIR 4359 Runtime ${suffix}`],
    )).rows[0].id as string;
    agentId = (await database.query(
      `INSERT INTO agent
         (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
       VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4) RETURNING id`,
      [workspaceId, `FIR 4359 Agent ${suffix}`, runtimeId, ownerUserId],
    )).rows[0].id as string;
    const autopilotTitle = `FIR 4359 Autopilot ${suffix}`;
    autopilotId = (await database.query(
      `INSERT INTO autopilot
         (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
       VALUES ($1, $2, 'agent', $3, 'active', 'run_only', 'member', $4) RETURNING id`,
      [workspaceId, autopilotTitle, agentId, memberUserId],
    )).rows[0].id as string;
    privateAutopilotId = (await database.query(
      `INSERT INTO autopilot
         (workspace_id, title, assignee_type, assignee_id, status, execution_mode,
          created_by_type, created_by_id, scope, owner_user_id, is_private)
       VALUES ($1, $2, 'agent', $3, 'active', 'run_only', 'member', $4, 'personal', $4, true)
       RETURNING id`,
      [workspaceId, `FIR 4359 private ceiling ${suffix}`, agentId, ownerUserId],
    )).rows[0].id as string;
    await database.query(
      `INSERT INTO autopilot_trigger
         (autopilot_id, kind, enabled, cron_expression, timezone, next_run_at, label)
       VALUES ($1, 'schedule', true, '0 9 * * *', 'UTC', NOW() + INTERVAL '1 day', 'FIR 4359 trigger')`,
      [autopilotId],
    );

    await setUserDecision(database, workspaceId, memberUserId, "create_autopilot", "deny");
    await setUserDecision(database, workspaceId, memberUserId, "trigger_autopilot", "deny");
    await openAsMember(page, member.token, memberUserId, workspaceSlug, "/autopilots");

    const newAutopilot = page.getByRole("button", { name: "New autopilot" });
    await expect(newAutopilot).toBeDisabled({ timeout: 30_000 });
    await expect(newAutopilot).toHaveAttribute("title", "Blocked by Permissions");

    await page.goto(`/${workspaceSlug}/autopilots/${autopilotId}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: autopilotTitle })).toBeVisible({ timeout: 30_000 });
    const statusSwitch = page.getByRole("switch", { name: "Pause autopilot" });
    await expect(statusSwitch).toBeDisabled({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Edit" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Run now" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Add trigger" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Delete trigger" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Delete autopilot" })).toBeDisabled();

    await setUserDecision(database, workspaceId, memberUserId, "create_autopilot", "allow");
    const privateTriggerAttempt = await fetch(`${API_BASE}/api/autopilots/${privateAutopilotId}/triggers`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${member.token}`,
        "X-Workspace-ID": workspaceId,
      },
      body: JSON.stringify({ kind: "schedule", cron_expression: "0 9 * * *", timezone: "UTC" }),
    });
    expect(privateTriggerAttempt.status).toBe(404);
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(statusSwitch).toBeEnabled({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Edit" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Add trigger" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Delete trigger" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Delete autopilot" })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Run now" })).toBeDisabled();

    await statusSwitch.click();
    await expect(page.getByText("Paused", { exact: true })).toBeVisible();
    await expect.poll(async () => (await database.query<{ status: string }>(
      "SELECT status FROM autopilot WHERE id = $1", [autopilotId],
    )).rows[0]?.status).toBe("paused");
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText("Paused", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole("switch", { name: "Activate autopilot" })).not.toBeChecked();
    await test.info().attach("autopilot-member-allowed-desktop", { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
    await page.setViewportSize({ width: 390, height: 844 });
    await test.info().attach("autopilot-member-allowed-mobile", { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
  } finally {
    if (memberUserId) await database.query(
      `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND layer = 'user' AND subject_id = $2 AND tool_key = ANY($3::text[])`,
      [workspaceId, memberUserId, ["create_autopilot", "trigger_autopilot"]],
    );
    if (privateAutopilotId) await database.query("DELETE FROM autopilot WHERE id = $1", [privateAutopilotId]);
    if (autopilotId) await database.query("DELETE FROM autopilot WHERE id = $1", [autopilotId]);
    if (agentId) await database.query("DELETE FROM agent WHERE id = $1", [agentId]);
    if (runtimeId) await database.query("DELETE FROM agent_runtime WHERE id = $1", [runtimeId]);
    if (memberUserId) {
      await database.query("DELETE FROM member WHERE workspace_id = $1 AND user_id = $2", [workspaceId, memberUserId]);
      await database.query('DELETE FROM "user" WHERE id = $1', [memberUserId]);
    }
    await database.end();
    await api.cleanup();
  }
});
