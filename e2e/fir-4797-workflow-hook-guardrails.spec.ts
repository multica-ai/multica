import "./env";
import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import pg from "pg";
import { TestApiClient } from "./fixtures";
import { loginAsDefault } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const SHOT_DIR = "e2e/screenshots/fir-4797";

test.beforeAll(() => mkdirSync(SHOT_DIR, { recursive: true }));

test("FIR-4797 — a second hook rejection completes with a visible named warning", async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 1440, height: 1000 });
  const slug = await loginAsDefault(page);
  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const suffix = Date.now().toString(36);
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email=$1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, name, runtime_mode, provider, status, device_info,
         metadata, last_seen_at
       ) VALUES ($1, $2, 'cloud', 'firtal-gateway', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspace.id, `FIR-4797 runtime ${suffix}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `FIR-4797 Agent ${suffix}`, runtimeId, userId],
    )
  ).rows[0].id as string;
  const issue = await api.createIssue(`FIR-4797 visible hook warning ${suffix}`);

  try {
    await database.query(
      `INSERT INTO agent_task_queue (
         agent_id, issue_id, runtime_id, status, priority, initiator_user_id,
         trigger_summary, result, started_at, completed_at
       ) VALUES ($1, $2, $3, 'completed', 0, $4, $5, $6::jsonb,
                 now() - interval '4 minutes', now() - interval '1 minute')`,
      [
        agentId,
        issue.id,
        runtimeId,
        userId,
        "Finish without a registered continuation",
        JSON.stringify({
          answer: "The original answer remains available.",
          completion_attempt: 2,
          completion_warning: {
            code: "workflow_gate_rejected",
            hook_id: "11111111-1111-1111-1111-111111111111",
            hook_name: "Require evidence before an agent run stops",
            requirement: "Create a wakeup",
            attempt: 2,
          },
        }),
      ],
    );

    await page.goto(`/${slug}/issues/${issue.id}`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("tab", { name: "Runs" }).click();
    await page.getByRole("button", { name: "Show past runs (1)" }).click();
    await expect(
      page.getByText(
        "Stopped by hook: Require evidence before an agent run stops",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Retry task" })).toHaveCount(0);

    const screenshot = `${SHOT_DIR}/named-completion-warning.png`;
    await page.screenshot({ path: screenshot, fullPage: false });
    await testInfo.attach("named-completion-warning", {
      path: screenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(`DELETE FROM agent_task_queue WHERE agent_id=$1`, [
      agentId,
    ]);
    await database.query(`DELETE FROM agent WHERE id=$1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id=$1`, [runtimeId]);
    await database.end();
    await api.cleanup();
  }
});
