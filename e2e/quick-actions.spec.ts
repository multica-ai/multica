import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

test("renders assistant quick actions and sends the hidden prompt", async ({
  page,
}) => {
  const api: TestApiClient = await createTestApi();
  const db = new pg.Client(DATABASE_URL);
  await db.connect();

  let sessionId: string | null = null;
  let agentId: string | null = null;
  let runtimeId: string | null = null;

  try {
    const workspace = (await api.getWorkspaces())[0];
    if (!workspace) throw new Error("E2E workspace missing");
    api.setWorkspaceId(workspace.id);
    api.setWorkspaceSlug(workspace.slug);

    const user = await db.query<{ id: string }>(
      `SELECT id::text FROM "user" WHERE email = $1 LIMIT 1`,
      [api.getEmail()],
    );
    const userId = user.rows[0]?.id;
    if (!userId) throw new Error("E2E user missing");

    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', 'e2e_quick_actions', 'online', $3, '{}'::jsonb, now())
       RETURNING id::text`,
      [workspace.id, `Quick actions ${Date.now()}`, "Quick actions E2E"],
    );
    runtimeId = runtime.rows[0]!.id;

    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (
         workspace_id, name, description, instructions, runtime_mode,
         runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id
       )
       VALUES ($1, 'Launch Copilot', 'Plans focused launches', '', 'cloud',
               '{}'::jsonb, $2, 'workspace', 1, $3)
       RETURNING id::text`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;

    const session = await db.query<{ id: string }>(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'Plan a focused launch', 'active')
       RETURNING id::text`,
      [workspace.id, agentId, userId],
    );
    sessionId = session.rows[0]!.id;

    await db.query(
      `INSERT INTO chat_message (
         chat_session_id, role, content, created_at, quick_actions
       )
       VALUES
         ($1, 'user', 'Help me plan a focused product launch.', now() - interval '1 second', '[]'::jsonb),
         ($1, 'assistant', $2, now(), $3::jsonb)`,
      [
        sessionId,
        "Start with one audience, one promise, and one proof point. Then build a two-week launch around a single measurable activation goal.",
        JSON.stringify([
          {
            label: "Draft the launch brief",
            prompt:
              "Draft a focused launch brief with one audience, one promise, one proof point, and one activation goal.",
            primary: true,
          },
          {
            label: "Build the two-week checklist",
            prompt: "Build a detailed two-week checklist for this launch.",
          },
          {
            label: "Define the activation metric",
            prompt: "Define a measurable activation metric for this launch.",
          },
        ]),
      ],
    );

    const token = api.getToken();
    if (!token) throw new Error("E2E token missing");
    await page.addInitScript(
      ({ authToken, activeSessionId }) => {
        localStorage.setItem("multica_token", authToken);
        localStorage.setItem("multica:chat:activeSessionId", activeSessionId);
        localStorage.setItem("multica:chat:isOpen", "false");
      },
      { authToken: token, activeSessionId: sessionId },
    );

    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${workspace.slug}/chat?session=${sessionId}`, {
      waitUntil: "domcontentloaded",
    });

    const primaryAction = page.getByRole("button", {
      name: "Draft the launch brief",
    });
    await expect(primaryAction).toBeVisible({ timeout: 30_000 });
    await expect(
      page.getByRole("button", { name: "Build the two-week checklist" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Define the activation metric" }),
    ).toBeVisible();

    if (process.env.QUICK_ACTIONS_SCREENSHOT_PATH) {
      await page.screenshot({
        path: process.env.QUICK_ACTIONS_SCREENSHOT_PATH,
        fullPage: true,
      });
    }

    await primaryAction.click();
    await expect(
      page.getByText(
        "Draft a focused launch brief with one audience, one promise, one proof point, and one activation goal.",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 15_000 });
    await expect(primaryAction).toBeDisabled();
  } finally {
    if (sessionId) {
      await db.query(`DELETE FROM agent_task_queue WHERE chat_session_id = $1`, [
        sessionId,
      ]);
      await db.query(`DELETE FROM chat_session WHERE id = $1`, [sessionId]);
    }
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId)
      await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
    await api.cleanup();
  }
});
