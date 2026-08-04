import "./env";
import { expect, type Page, test } from "@playwright/test";
import pg from "pg";

import { createTestApi, loginAsDefault } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function dismissAttributionSurvey(page: Page) {
  const dialog = page.getByRole("dialog", {
    name: "How did you hear about Multica?",
  });
  if (await dialog.isVisible()) {
    await dialog.getByRole("button", { name: "Skip", exact: true }).click();
    await expect(dialog).toBeHidden();
  }
}

async function preventAttributionSurvey(page: Page, userId: string) {
  await page.addInitScript(
    ({ storageKey }) => localStorage.setItem(storageKey, "3"),
    { storageKey: `multica.source_backfill.dismiss.${userId}` },
  );
}

test("Settings Permissions writes the canonical policy and persists after reload", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  const database = new pg.Client(DATABASE_URL);
  await database.connect();

  const toolKey = "tools:FIR3388PermissionsWiring";
  const title = "FIR 3388 Permissions wiring";
  const flagKeys = ["cerebro_tool_policy", "cerebro_agent_page_redesign"];
  const previousFlags = await database.query<{
    flag_key: string;
    enabled: boolean;
  }>(
    `SELECT flag_key, enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = ANY($2::text[])`,
    [workspaceId, flagKeys],
  );
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
      [workspaceId, `FIR 3388 Permissions runtime ${Date.now()}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspaceId, `FIR 3388 Permissions agent ${Date.now()}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  try {
    for (const flagKey of flagKeys) {
      await api.setWorkspaceFeatureFlag(flagKey, true);
    }
    await database.query(
      `INSERT INTO cerebro_capability
         (workspace_id, capability_key, title, category, description, source)
       VALUES ($1, $2, $3, 'Built-in tools',
               'Proves that the visible Permissions control writes the canonical policy.', 'e2e')
       ON CONFLICT (workspace_id, capability_key)
       DO UPDATE SET title = EXCLUDED.title, last_reported_at = NOW()`,
      [workspaceId, toolKey, title],
    );

    await preventAttributionSurvey(page, userId);
    const workspaceSlug = await loginAsDefault(page, {
      workspaceReadyTimeout: 60_000,
    });
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });
    await dismissAttributionSurvey(page);

    await expect(
      page.getByRole("heading", { name: "How access is decided" }),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Settings → Permissions is the live authoring source. A task freezes its Task Mandate when the run starts. A later Deny or safety ceiling can tighten the active run, but a later Allow never widens its frozen Task Mandate. Start a new task to capture newly allowed access.",
        { exact: true },
      ),
    ).toBeVisible();

    const row = page.getByTestId(`tool-card-${toolKey}`);
    await expect(row).toBeVisible();
    await expect(row.getByRole("button", { name: "Decision: Allow" })).toBeVisible();

    await row.getByRole("button", { name: "Decision: Allow" }).click();
    await page.getByTestId(`catalog-decision-${toolKey}-deny`).click();
    await expect(row.getByRole("button", { name: "Decision: Deny" })).toBeVisible();

    const stored = await database.query<{ setting: string }>(
      `SELECT setting
         FROM cerebro_tool_policy
        WHERE workspace_id = $1
          AND tool_key = $2
          AND layer = 'workspace'
          AND subject_id = $1
          AND resource_pattern = ''`,
      [workspaceId, toolKey],
    );
    expect(stored.rows).toEqual([{ setting: "deny" }]);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(
      page
        .getByTestId(`tool-card-${toolKey}`)
        .getByRole("button", { name: "Decision: Deny" }),
    ).toBeVisible({ timeout: 30_000 });

    // The same Settings decision must reach the agent's own Capabilities card
    // with an actionable denial. This joins authoring, the visible agent view,
    // and the error wording in one browser journey instead of testing each
    // surface in isolation.
    await page.goto(`/${workspaceSlug}/agents/${agentId}`, {
      waitUntil: "domcontentloaded",
    });
    await expect(async () => {
      await page.getByRole("button", { name: "Capabilities" }).click();
      await expect(
        page.getByRole("heading", { name: "Tools", exact: true }),
      ).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 30_000 });
    const toolsSection = page
      .getByRole("heading", { name: "Tools", exact: true })
      .locator("..")
      .locator("..");
    const deniedCapability = toolsSection
      .locator("span[title]")
      .filter({ hasText: new RegExp(`^${title}\\s*·\\s*blocked$`) });
    await expect(deniedCapability).toHaveCount(1);
    await expect(deniedCapability).toHaveAttribute(
      "title",
      /deny.*allowed: no.*callable: no.*blocked:/i,
    );

    const evidencePath =
      process.env.PLAYWRIGHT_EVIDENCE_PATH ??
      test.info().outputPath("settings-permissions-deny-after-reload.png");
    const screenshot = await page.screenshot({
      fullPage: true,
      path: evidencePath,
    });
    await test.info().attach("settings-permissions-deny-after-reload", {
      body: screenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(
      `DELETE FROM cerebro_tool_policy
        WHERE workspace_id = $1 AND tool_key = $2`,
      [workspaceId, toolKey],
    );
    await database.query(
      `DELETE FROM cerebro_capability
        WHERE workspace_id = $1 AND capability_key = $2`,
      [workspaceId, toolKey],
    );
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.query(
      `DELETE FROM cerebro_feature_flags
        WHERE workspace_id = $1
          AND user_id = '00000000-0000-0000-0000-000000000000'
          AND flag_key = ANY($2::text[])`,
      [workspaceId, flagKeys],
    );
    for (const previous of previousFlags.rows) {
      await database.query(
        `INSERT INTO cerebro_feature_flags
           (workspace_id, user_id, flag_key, enabled)
         VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3)
         ON CONFLICT (workspace_id, user_id, flag_key)
         DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
        [workspaceId, previous.flag_key, previous.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});

test("Settings identifies workspace-governed machine intake and keeps its off-switch authorable", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const api = await createTestApi();

  try {
    await api.setWorkspaceFeatureFlag("cerebro_tool_policy", true);
    await api.setWorkspaceFeatureFlag("cerebro_agent_page_redesign", true);
    const workspaceSlug = await loginAsDefault(page);
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });
    await dismissAttributionSurvey(page);
    await page.setViewportSize({ width: 390, height: 844 });

    const row = page.getByTestId("tool-card-autopilot_webhook");
    await expect(row).toBeVisible();
    await expect(row.getByText("Governed by", { exact: true })).toBeVisible();
    const owner = row.getByText("Autopilot webhook secret", { exact: true });
    await expect(owner).toBeVisible();
    await expect(row.getByText("Workspace off-switch", { exact: true })).toBeVisible();
    const [ownerBox, rowBox] = await Promise.all([owner.boundingBox(), row.boundingBox()]);
    expect(ownerBox).not.toBeNull();
    expect(rowBox).not.toBeNull();
    expect(ownerBox!.x + ownerBox!.width).toBeLessThanOrEqual(rowBox!.x + rowBox!.width);
    await expect(row.getByRole("button", { name: /^Decision:/ })).toBeEnabled();
  } finally {
    await api.cleanup();
  }
});

test("Permission profiles are separated, explained, assigned, and enforced through the same permission decision", async ({
  page,
}) => {
  test.setTimeout(240_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  if (!workspaceId) throw new Error("E2E workspace was not resolved");
  const database = new pg.Client(DATABASE_URL);
  await database.connect();

  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  const toolKey = `tools:fir3388-role-${suffix}`;
  const toolTitle = `FIR 3388 Role tool ${suffix}`;
  const agentName = `FIR 3388 Role agent ${suffix}`;
  const roleName = `FIR 3388 Reviewer ${suffix}`;
  const flagKeys = [
    "cerebro_tool_policy",
    "cerebro_agent_page_redesign",
    "cerebro_permission_detail",
  ];
  const previousFlags = await database.query<{
    flag_key: string;
    enabled: boolean;
  }>(
    `SELECT flag_key, enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = ANY($2::text[])`,
    [workspaceId, flagKeys],
  );

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
       ) VALUES ($1, $2, 'local', 'codex', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspaceId, `FIR 3388 Role runtime ${suffix}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspaceId, agentName, runtimeId, userId],
    )
  ).rows[0].id as string;

  try {
    for (const flagKey of flagKeys) {
      await api.setWorkspaceFeatureFlag(flagKey, true);
    }
    const capabilityId = (
      await database.query(
        `INSERT INTO cerebro_capability (
           workspace_id, capability_key, title, category, description, source,
           metadata, last_reported_at, updated_at
         ) VALUES (
           $1, $2, $3, 'Built-in tools',
           'Role parity proof for Settings, Capabilities and call-time policy.',
           'runtime_report', '{}'::jsonb, now(), now()
         )
         RETURNING id`,
        [workspaceId, toolKey, toolTitle],
      )
    ).rows[0].id as string;
    await database.query(
      `INSERT INTO cerebro_capability_subject (
         capability_id, workspace_id, subject_type, subject_id, relation,
         metadata, first_seen_at, last_seen_at
       ) VALUES ($1, $2, 'runtime', $3, 'reporter', '{}'::jsonb, now(), now())`,
      [capabilityId, workspaceId, runtimeId],
    );

    await preventAttributionSurvey(page, userId);
    const workspaceSlug = await loginAsDefault(page, { workspaceReadyTimeout: 60_000 });
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });
    await dismissAttributionSurvey(page);
    await expect(page.getByRole("tab", { name: "Access rules", exact: true })).toHaveAttribute(
      "aria-selected",
      "true",
      { timeout: 60_000 },
    );
    await expect(page.getByRole("tab", { name: "Permission profiles", exact: true })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Security controls", exact: true })).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Permission profiles", exact: true }),
    ).toBeHidden();
    await page.getByRole("tab", { name: "Permission profiles", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "When should I use a Permission profile?", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Use the agent's Tools page", { exact: false })).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Permission profiles", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Create profile", exact: true }).click();

    const editor = page.getByRole("dialog", { name: "Create profile" });
    await expect(editor).toBeVisible({ timeout: 15_000 });
    await expect(
      editor.getByText("No permission overrides yet.", { exact: true }),
    ).toBeVisible();
    await expect(editor.getByRole("combobox", { name: / decision$/ })).toHaveCount(0);
    expect((await editor.boundingBox())?.width).toBeGreaterThan(640);
    await editor.getByLabel("Name", { exact: true }).fill(roleName);
    await editor
      .getByLabel("Description", { exact: true })
      .fill("Reusable role that proves one effective permission truth.");
    await editor.getByLabel("Search permissions").fill(toolTitle);
    await editor
      .getByRole("combobox", { name: `${toolTitle} decision`, exact: true })
      .click();
    await page.getByRole("option", { name: "Deny", exact: true }).click();
    await editor.getByLabel("Search permissions").fill("");
    await expect(
      editor.getByRole("combobox", { name: `${toolTitle} decision`, exact: true }),
    ).toContainText(/deny/i);
    const desktopEditorScreenshot = await page.screenshot({
      path: test.info().outputPath("permission-profile-editor-desktop.png"),
    });
    await test.info().attach("permission-profile-editor-desktop", {
      body: desktopEditorScreenshot,
      contentType: "image/png",
    });
    await dismissAttributionSurvey(page);
    await editor.getByRole("button", { name: "Save version", exact: true }).click();

    await expect(
      page.getByRole("heading", { name: roleName, exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Version 1", { exact: true }).last()).toBeVisible();
    await page.getByRole("combobox", { name: "Profile assignee" }).click();
    await page
      .locator('[data-slot="select-content"]')
      .getByRole("option", { name: agentName, exact: true })
      .click();
    await page.getByRole("button", { name: "Assign", exact: true }).click();
    await expect(
      page.getByRole("button", { name: `Remove ${agentName}`, exact: true }),
    ).toBeVisible();
    const effectiveAccess = page
      .getByRole("heading", { name: "Effective access", exact: true })
      .locator("..");
    await expect(effectiveAccess).toContainText(toolTitle);
    await expect(effectiveAccess).toContainText(/deny/i);
    await expect(effectiveAccess).toContainText(`via role ${roleName} v1`);

    await page.goto(`/${workspaceSlug}/agents/${agentId}`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("button", { name: "Capabilities", exact: true }).click();
    const roleCapability = page
      .locator("span[title]")
      .filter({ hasText: new RegExp(`^${toolTitle}\\s*·\\s*blocked$`) });
    await expect(roleCapability).toHaveCount(1);
    await expect(roleCapability).toHaveAttribute(
      "title",
      new RegExp(`deny.*via role ${roleName} v1`, "i"),
    );

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("tab", { name: "Permission profiles", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Permission profiles", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    const mobileEditor = page.getByRole("dialog", { name: `Edit ${roleName}` });
    await expect(mobileEditor).toBeVisible();
    await expect(mobileEditor).toHaveCSS("opacity", "1");
    expect((await mobileEditor.boundingBox())?.height).toBeLessThanOrEqual(844);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    const mobileEditorScreenshot = await mobileEditor.screenshot({
      path: test.info().outputPath("permission-profile-editor-mobile.png"),
    });
    await test.info().attach("permission-profile-editor-mobile", {
      body: mobileEditorScreenshot,
      contentType: "image/png",
    });
    await mobileEditor.getByRole("button", { name: "Cancel", exact: true }).click();

    await page.goto(
      `/${workspaceSlug}/cerebro/permissions/${encodeURIComponent(toolKey)}`,
      { waitUntil: "domcontentloaded" },
    );
    const detailMain = page.locator("main.overflow-y-auto");
    await expect(detailMain).toHaveCSS("overflow-y", "auto", { timeout: 60_000 });
    const rolesTab = page.getByRole("tab", { name: /^Profiles\b/ });
    await rolesTab.click();
    await expect(rolesTab).toHaveAttribute("aria-selected", "true");
    await expect(
      page.getByRole("button", {
        name: /Which Permission profiles and assignments apply\?/,
      }),
    ).toHaveAttribute("aria-pressed", "true");
    await expect(
      page.getByRole("heading", { name: roleName, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("tabpanel", { name: /^Profiles\b/ }).getByText(agentName, { exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);

    const evidencePath = test.info().outputPath("role-capability-parity.png");
    const screenshot = await page.screenshot({
      fullPage: true,
      path: evidencePath,
    });
    await test.info().attach("role-capability-parity", {
      body: screenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(
      `DELETE FROM cerebro_role
        WHERE workspace_id = $1 AND name = $2`,
      [workspaceId, roleName],
    );
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.query(
      `DELETE FROM cerebro_capability
        WHERE workspace_id = $1 AND capability_key = $2`,
      [workspaceId, toolKey],
    );
    await database.query(
      `DELETE FROM cerebro_feature_flags
        WHERE workspace_id = $1
          AND user_id = '00000000-0000-0000-0000-000000000000'
          AND flag_key = ANY($2::text[])`,
      [workspaceId, flagKeys],
    );
    for (const previous of previousFlags.rows) {
      await database.query(
        `INSERT INTO cerebro_feature_flags
           (workspace_id, user_id, flag_key, enabled)
         VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3)
         ON CONFLICT (workspace_id, user_id, flag_key)
         DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
        [workspaceId, previous.flag_key, previous.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});

test("Ask enforcement keeps the Approvals path visible", async ({ page }) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  if (!workspaceId) throw new Error("E2E workspace was not resolved");
  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const flagKeys = ["cerebro_approvals", "cerebro_approval_gate"];
  const previousFlags = await database.query<{
    flag_key: string;
    enabled: boolean;
  }>(
    `SELECT flag_key, enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = ANY($2::text[])`,
    [workspaceId, flagKeys],
  );
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;

  try {
    await api.setWorkspaceFeatureFlag("cerebro_approvals", false);
    await api.setWorkspaceFeatureFlag("cerebro_approval_gate", true);
    await preventAttributionSurvey(page, userId);
    const workspaceSlug = await loginAsDefault(page);
    const approvals = page.getByRole("link", {
      name: "Approvals",
      exact: true,
    });
    await expect(approvals).toBeVisible();
    await approvals.click();
    await expect(page).toHaveURL(
      new RegExp(`/${workspaceSlug}/approvals$`),
      { timeout: 60_000 },
    );
    await expect(
      page.getByRole("heading", { name: "Approvals", exact: true }),
    ).toBeVisible();
    const screenshot = await page.screenshot({
      fullPage: true,
      path: test.info().outputPath("ask-approval-path.png"),
    });
    await test.info().attach("ask-approval-path", {
      body: screenshot,
      contentType: "image/png",
    });
  } finally {
    await database.query(
      `DELETE FROM cerebro_feature_flags
        WHERE workspace_id = $1
          AND user_id = '00000000-0000-0000-0000-000000000000'
          AND flag_key = ANY($2::text[])`,
      [workspaceId, flagKeys],
    );
    for (const previous of previousFlags.rows) {
      await database.query(
        `INSERT INTO cerebro_feature_flags
           (workspace_id, user_id, flag_key, enabled)
         VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3)
         ON CONFLICT (workspace_id, user_id, flag_key)
         DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
        [workspaceId, previous.flag_key, previous.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});

test("Approvals denies a non-admin workspace member", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  const workspaceSlug = api.getWorkspaceSlug();
  if (!workspaceId || !workspaceSlug) {
    throw new Error("E2E workspace was not resolved");
  }

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const previousFlag = await database.query<{ enabled: boolean }>(
    `SELECT enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = '00000000-0000-0000-0000-000000000000'
        AND flag_key = 'cerebro_approvals'`,
    [workspaceId],
  );
  const email = `fir-3388-approvals-member-${Date.now()}@multica.ai`;
  let memberUserId = "";

  try {
    await api.setWorkspaceFeatureFlag("cerebro_approvals", true);
    const member = await api.loginSecondaryUser(
      email,
      "FIR 3388 Approvals Member",
    );
    memberUserId = member.userId;
    await page.addInitScript(
      ({ token, userId }) => {
        localStorage.setItem("multica_token", token);
        localStorage.setItem(`multica.source_backfill.dismiss.${userId}`, "3");
      },
      { token: member.token, userId: member.userId },
    );

    await page.goto(`/${workspaceSlug}/approvals`, {
      waitUntil: "domcontentloaded",
    });
    await expect(
      page.getByRole("heading", { name: "Access denied", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Only workspace owners and admins can handle approval requests.",
        { exact: true },
      ),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("approvals-non-admin-denied.png"),
      fullPage: true,
    });
  } finally {
    if (memberUserId) {
      await database.query(
        `DELETE FROM member
          WHERE workspace_id = $1 AND user_id = $2`,
        [workspaceId, memberUserId],
      );
    }
    await database.query(
      `DELETE FROM cerebro_feature_flags
        WHERE workspace_id = $1
          AND user_id = '00000000-0000-0000-0000-000000000000'
          AND flag_key = 'cerebro_approvals'`,
      [workspaceId],
    );
    if (previousFlag.rowCount === 1) {
      await database.query(
        `INSERT INTO cerebro_feature_flags
           (workspace_id, user_id, flag_key, enabled)
         VALUES ($1, '00000000-0000-0000-0000-000000000000',
                 'cerebro_approvals', $2)
         ON CONFLICT (workspace_id, user_id, flag_key)
         DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
        [workspaceId, previousFlag.rows[0]!.enabled],
      );
    }
    await database.end();
    await api.cleanup();
  }
});
