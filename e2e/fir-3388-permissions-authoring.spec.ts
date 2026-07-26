import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi, loginAsDefault } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

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

    const workspaceSlug = await loginAsDefault(page);
    await page.goto(`/${workspaceSlug}/settings?tab=permissions`, {
      waitUntil: "domcontentloaded",
    });

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
    ).toBeVisible();

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
