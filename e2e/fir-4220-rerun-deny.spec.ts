import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi, loginAsDefault } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

// FIR-4220: `rerun_issue` used to be an advisory "Managed externally" row — the
// Settings control was disabled and a stored Deny changed nothing. This spec
// proves the whole loop is now real: the row is EDITABLE in Settings →
// Permissions, authoring Deny at the workspace layer stores the canonical
// policy, and an agent-actor call to POST /api/issues/{id}/rerun is actually
// blocked by that policy (members keep their own role/membership gates).
test("Deny on rerun_issue in Settings blocks the agent rerun call", async ({ page }) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  const database = new pg.Client(DATABASE_URL);
  await database.connect();

  const flagKeys = ["cerebro_tool_policy", "cerebro_agent_page_redesign"];
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, name, runtime_mode, provider, status, device_info,
         metadata, last_seen_at
       ) VALUES ($1, $2, 'cloud', 'firtal-gateway', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspaceId, `FIR 4220 rerun runtime ${Date.now()}`],
    )
  ).rows[0].id as string;
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspaceId, `FIR 4220 rerun agent ${Date.now()}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  try {
    for (const flagKey of flagKeys) {
      await api.setWorkspaceFeatureFlag(flagKey, true);
    }
    const issue = await api.createIssue("FIR-4220 rerun deny target");
    // resolveActor only trusts X-Agent-ID when X-Task-ID names a real task of
    // that agent — create one so the rerun call resolves as an agent actor.
    const taskId = (
      await database.query(
        `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
         VALUES ($1, $2, $3, 'completed', 0)
         RETURNING id`,
        [agentId, runtimeId, issue.id],
      )
    ).rows[0].id as string;

    const rerunAsAgent = () =>
      fetch(`${API_BASE}/api/issues/${issue.id}/rerun`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${api.getToken()}`,
          "X-Workspace-ID": workspaceId,
          "X-Agent-ID": agentId,
          "X-Task-ID": taskId,
        },
        body: JSON.stringify({}),
      });

    // Baseline: without an authored Deny the call must not be policy-blocked
    // (whatever else it returns), so the 403 below is provably the policy's.
    const before = await rerunAsAgent();
    if (before.status === 403) {
      const body = await before.json();
      expect(body.code).not.toBe("platform_action_denied");
    }

    // Author Deny through the real Settings → Permissions UI: the row that was
    // read-only under the advisory design must now carry a live control.
    const workspaceSlug = await loginAsDefault(page);
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });
    const row = page.getByTestId("tool-card-rerun_issue");
    await expect(row).toBeVisible();
    const decision = row.getByRole("button", { name: /^Decision:/ });
    await expect(decision).toBeEnabled();
    await decision.click();
    await page.getByTestId("catalog-decision-rerun_issue-deny").click();
    await expect(row.getByRole("button", { name: "Decision: Deny" })).toBeVisible();

    const stored = await database.query<{ setting: string }>(
      `SELECT setting
         FROM cerebro_tool_policy
        WHERE workspace_id = $1
          AND tool_key = 'rerun_issue'
          AND layer = 'workspace'
          AND subject_id = $1
          AND resource_pattern = ''`,
      [workspaceId],
    );
    expect(stored.rows).toEqual([{ setting: "deny" }]);

    // The authored Deny is the live gate for the agent actor.
    const denied = await rerunAsAgent();
    expect(denied.status).toBe(403);
    const deniedBody = await denied.json();
    expect(deniedBody.code).toBe("platform_action_denied");
    expect(deniedBody.capability).toBe("rerun_issue");
  } finally {
    await database.query(
      `DELETE FROM cerebro_tool_policy
        WHERE workspace_id = $1 AND tool_key = 'rerun_issue'`,
      [workspaceId],
    );
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    for (const flagKey of flagKeys) {
      await api.setWorkspaceFeatureFlag(flagKey, false);
    }
    await database.end();
    await api.cleanup();
  }
});
