import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

test("Capabilities shows the effective allow and deny policy after reload", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const slug = await loginAsDefault(page);
  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const previousFeatureFlag = await database.query<{ enabled: boolean }>(
    `SELECT enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = 'cerebro_agent_page_redesign'`,
    [workspace.id],
  );
  await api.setWorkspaceFeatureFlag("cerebro_agent_page_redesign", true);
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
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
      [workspace.id, `FIR-3388 runtime ${Date.now()}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `FIR-3388 Agent ${Date.now()}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  try {
    await database.query(
      `INSERT INTO cerebro_tool_policy (
         workspace_id, tool_key, layer, subject_id, setting, resource_pattern
       ) VALUES ($1, 'create_issue', 'agent', $2, 'allow', ''),
                ($1, 'delete_issue', 'agent', $2, 'deny', '')`,
      [workspace.id, agentId],
    );

    await page.goto(`/${slug}/agents/${agentId}`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("button", { name: "Capabilities" }).click();

    const allowed = page
      .locator('[title*="allow"]')
      .filter({ hasText: /^Create issue/ });
    const denied = page
      .locator('[title*="deny"]')
      .filter({ hasText: /^Delete issue/ });
    await expect(allowed).toBeVisible();
    await expect(denied).toBeVisible();

    const assertEffectiveAccess = async () => {
      const capabilities = await api.getAgentCapabilities(agentId);
      expect(
        capabilities.tools.find((tool) => tool.key === "create_issue"),
      ).toMatchObject({
        permission: "allow",
        allowed: true,
        callable: true,
      });
      expect(
        capabilities.tools.find((tool) => tool.key === "delete_issue"),
      ).toMatchObject({
        permission: "deny",
        allowed: false,
        callable: false,
      });
    };
    await assertEffectiveAccess();

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Capabilities" }).click();
    await expect(allowed).toBeVisible();
    await expect(denied).toBeVisible();
    await assertEffectiveAccess();
    const evidencePath = process.env.PLAYWRIGHT_EVIDENCE_PATH;
    const screenshot = await page.screenshot({
      fullPage: true,
      path: evidencePath,
    });
    await test.info().attach("capabilities-after-reload", {
      body: screenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(`DELETE FROM cerebro_tool_policy WHERE subject_id = $1`, [
      agentId,
    ]);
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    if (previousFeatureFlag.rowCount === 0) {
      await database.query(
        `DELETE FROM cerebro_feature_flags
          WHERE workspace_id = $1
            AND user_id = '00000000-0000-0000-0000-000000000000'
            AND flag_key = 'cerebro_agent_page_redesign'`,
        [workspace.id],
      );
    } else {
      await database.query(
        `UPDATE cerebro_feature_flags
            SET enabled = $2, updated_at = NOW()
          WHERE workspace_id = $1
            AND user_id = '00000000-0000-0000-0000-000000000000'
            AND flag_key = 'cerebro_agent_page_redesign'`,
        [workspace.id, previousFeatureFlag.rows[0]!.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});
