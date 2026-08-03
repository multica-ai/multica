import "./env";
import { expect, type Page, test } from "@playwright/test";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function openCapabilitiesTab(page: Page) {
  await expect(async () => {
    const dialog = page.getByRole("dialog", {
      name: "How did you hear about Multica?",
    });
    if (await dialog.isVisible()) {
      await dialog.getByRole("button", { name: "Skip" }).click();
    }
    await page.getByRole("button", { name: "Capabilities" }).click();
    await expect(
      page.getByRole("heading", { name: "Tools", exact: true }),
    ).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 30_000 });
}

test("Capabilities shows the effective allow and deny policy after reload", async ({
  page,
}) => {
  test.setTimeout(180_000);
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
    await openCapabilitiesTab(page);

    const toolsSection = page
      .getByRole("heading", { name: "Tools", exact: true })
      .locator("..")
      .locator("..");
    const allowed = toolsSection
      .locator("span[title]")
      .filter({ hasText: /^Create issue\s*·\s*callable$/ });
    const denied = toolsSection
      .locator("span[title]")
      .filter({ hasText: /^Delete issue\s*·\s*blocked$/ });
    await expect(allowed).toHaveCount(1);
    await expect(denied).toHaveCount(1);
    await expect(allowed).toHaveAttribute("title", /^allow(?:\s|·)/);
    await expect(denied).toHaveAttribute("title", /^deny(?:\s|·)/);
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

    const featureFlagsReady = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        response.url().includes("/feature-flags") &&
        response.ok(),
    );
    await page.reload({ waitUntil: "domcontentloaded" });
    await featureFlagsReady;
    await openCapabilitiesTab(page);
    await expect(allowed).toBeVisible();
    await expect(denied).toBeVisible();
    await assertEffectiveAccess();
    const evidencePath =
      process.env.PLAYWRIGHT_EVIDENCE_PATH ??
      test.info().outputPath("capabilities-after-reload.png");
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

test("Codex uses one permission identity in inventory, Capabilities, observed access, and call-time aliases", async ({
  page,
}) => {
  test.setTimeout(180_000);
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
  const previousAccessDiagnosticsFlag = await database.query<{
    enabled: boolean;
  }>(
    `SELECT enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = 'cerebro_access_diagnostics'`,
    [workspace.id],
  );
  await api.setWorkspaceFeatureFlag("cerebro_agent_page_redesign", true);
  await api.setWorkspaceFeatureFlag("cerebro_access_diagnostics", true);
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;
  await page.addInitScript(
    ({ storageKey }) => localStorage.setItem(storageKey, "3"),
    { storageKey: `multica.source_backfill.dismiss.${userId}` },
  );
  const slug = await loginAsDefault(page);
  const suffix = Date.now().toString(36);
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, name, runtime_mode, provider, status, device_info,
         metadata, last_seen_at
       ) VALUES ($1, $2, 'local', 'codex', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspace.id, `FIR-3388 Codex runtime ${suffix}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `FIR-3388 Codex agent ${suffix}`, runtimeId, userId],
    )
  ).rows[0].id as string;
  const issue = await api.createIssue(`FIR-3388 Codex proof ${suffix}`);

  try {
    await database.query(
      `INSERT INTO cerebro_capability (
         workspace_id, capability_key, title, category, description, source,
         metadata, last_reported_at, updated_at
       ) VALUES (
         $1, 'tools:bash', 'bash', 'Built-in tools',
         'Codex shell tool reported by the live runtime.', 'runtime_report',
         '{}'::jsonb, now(), now()
       )
       ON CONFLICT (workspace_id, capability_key)
       DO UPDATE SET
         title = EXCLUDED.title,
         category = EXCLUDED.category,
         description = EXCLUDED.description,
         source = EXCLUDED.source,
         last_reported_at = now(),
         updated_at = now()`,
      [workspace.id],
    );
    await database.query(
      `INSERT INTO cerebro_capability_subject (
         capability_id, workspace_id, subject_type, subject_id, relation,
         metadata, first_seen_at, last_seen_at
       )
       SELECT id, workspace_id, 'runtime', $2, 'reporter', '{}'::jsonb, now(), now()
         FROM cerebro_capability
        WHERE workspace_id = $1 AND capability_key = 'tools:bash'
       ON CONFLICT (capability_id, subject_type, subject_id, relation)
       DO UPDATE SET last_seen_at = now()`,
      [workspace.id, runtimeId],
    );
    await database.query(
      `INSERT INTO cerebro_tool_policy (
         workspace_id, tool_key, layer, subject_id, setting, resource_pattern
       ) VALUES ($1, 'tools:bash', 'agent', $2, 'allow', '')`,
      [workspace.id, agentId],
    );
    const taskId = (
      await database.query(
        `INSERT INTO agent_task_queue (
           agent_id, issue_id, runtime_id, status, priority, initiator_user_id,
           started_at, completed_at
         ) VALUES ($1, $2, $3, 'completed', 0, $4, now(), now())
         RETURNING id`,
        [agentId, issue.id, runtimeId, userId],
      )
    ).rows[0].id as string;
    await database.query(
      `INSERT INTO task_message (task_id, seq, type, tool, content)
       VALUES ($1, 1, 'tool', 'exec_command', 'safe Codex alias proof')`,
      [taskId],
    );
    await database.query(
      `INSERT INTO cerebro_task_mandate (
         task_id, workspace_id, agent_id, allowed_tools, issued_at, expires_at
       ) VALUES ($1, $2, $3, '["tools:bash"]'::jsonb, now() - interval '2 minutes',
                 now() - interval '1 minute')`,
      [taskId, workspace.id, agentId],
    );

    await page.goto(`/${slug}/agents/${agentId}`, {
      waitUntil: "domcontentloaded",
    });
    await openCapabilitiesTab(page);

    const toolsSection = page
      .getByRole("heading", { name: "Tools", exact: true })
      .locator("..")
      .locator("..");
    const bash = toolsSection
      .locator("span[title]")
      .filter({ hasText: /^bash\s*·\s*callable$/ });
    await expect(bash).toHaveCount(1);
    await expect(bash).toHaveAttribute(
      "title",
      /allow.*available: yes.*callable: yes/i,
    );
    await expect(
      toolsSection.getByText(/1 discovered/i),
    ).toBeVisible();

    const observedSection = page
      .getByRole("heading", { name: "Observed access", exact: true })
      .locator("..")
      .locator("..");
    const observedAlias = observedSection
      .locator("span[title]")
      .filter({ hasText: /^exec_command\s*·\s*1$/ });
    await expect(observedAlias).toHaveCount(1);
    await expect(observedAlias).toHaveAttribute("title", /policy: allowed/i);
    await expect(observedSection).not.toContainText("Reviewer:");

    const capabilities = await api.getAgentCapabilities(agentId);
    expect(
      capabilities.tools.find((tool) => tool.key === "tools:bash"),
    ).toMatchObject({
      permission: "allow",
      allowed: true,
      available: true,
      enforced: true,
      callable: true,
      verified: false,
      availability: {
        level: "discovered",
        proven: false,
      },
    });
    expect(capabilities.availability).toMatchObject({
      runtime_type: "local",
      status: "known",
      discovered: 1,
    });
    expect(capabilities.observed_access).toMatchObject({
      status: "known",
      drift_count: 0,
      tools: [
        {
          name: "exec_command",
          permission: "allow",
          status: "allowed",
          drift: false,
        },
      ],
    });

    const evidencePath =
      process.env.PLAYWRIGHT_CODEX_EVIDENCE_PATH ??
      test.info().outputPath("codex-permission-identity.png");
    const screenshot = await page.screenshot({
      fullPage: true,
      path: evidencePath,
    });
    await test.info().attach("codex-permission-identity", {
      body: screenshot,
      contentType: "image/png",
    });
    await observedSection.scrollIntoViewIfNeeded();
    const observedEvidencePath = test
      .info()
      .outputPath("codex-observed-access.png");
    const observedScreenshot = await observedSection.screenshot({
      path: observedEvidencePath,
    });
    await test.info().attach("codex-observed-access", {
      body: observedScreenshot,
      contentType: "image/png",
    });

    // The historical task must expose the exact immutable tool envelope that
    // was enforced for that run, not today's policy or a generic description.
    await page
      .getByRole("button", { name: "Tasks", exact: true })
      .first()
      .click();
    await page.getByRole("button", { name: "Activity", exact: true }).click();
    const transcriptButton = page.getByRole("button", {
      name: "View transcript",
      exact: true,
    });
    await expect(transcriptButton).toHaveCount(1);
    await transcriptButton.click();
    const taskAccess = page.getByText("Task access", {
      exact: true,
    });
    await expect(taskAccess).toBeVisible();
    await expect(
      page.getByText("1 capabilities · ended", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Observation only", { exact: true }).first()).toBeVisible();
    await expect(page.getByText("Task access expired", { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "Settings → Permissions is live: a later Deny or safety ceiling can still tighten this run, but a later Allow cannot widen its frozen Task Mandate. Start a new task to capture newly allowed access.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(page.getByText("tools:bash", { exact: true })).toBeVisible();
    const taskAccessPath = test.info().outputPath("codex-task-access.png");
    const taskAccessScreenshot = await page
      .getByRole("dialog")
      .screenshot({ path: taskAccessPath });
    await test.info().attach("codex-task-access", {
      body: taskAccessScreenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(`DELETE FROM cerebro_tool_policy WHERE subject_id = $1`, [
      agentId,
    ]);
    await database.query(
      `DELETE FROM cerebro_capability_subject
        WHERE workspace_id = $1
          AND subject_type = 'runtime'
          AND subject_id = $2
          AND capability_id IN (
            SELECT id FROM cerebro_capability
             WHERE workspace_id = $1 AND capability_key = 'tools:bash'
          )`,
      [workspace.id, runtimeId],
    );
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.query(
      `DELETE FROM cerebro_capability
        WHERE workspace_id = $1
          AND capability_key = 'tools:bash'
          AND source = 'runtime_report'
          AND NOT EXISTS (
            SELECT 1 FROM cerebro_capability_subject
             WHERE capability_id = cerebro_capability.id
          )`,
      [workspace.id],
    );
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
    if (previousAccessDiagnosticsFlag.rowCount === 0) {
      await database.query(
        `DELETE FROM cerebro_feature_flags
          WHERE workspace_id = $1
            AND user_id = '00000000-0000-0000-0000-000000000000'
            AND flag_key = 'cerebro_access_diagnostics'`,
        [workspace.id],
      );
    } else {
      await database.query(
        `UPDATE cerebro_feature_flags
            SET enabled = $2, updated_at = NOW()
          WHERE workspace_id = $1
            AND user_id = '00000000-0000-0000-0000-000000000000'
            AND flag_key = 'cerebro_access_diagnostics'`,
        [workspace.id, previousAccessDiagnosticsFlag.rows[0]!.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});
