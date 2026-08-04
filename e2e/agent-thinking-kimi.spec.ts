import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi, waitForPageText } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

test("Kimi thinking level is selectable and survives a reload", async ({
  page,
}) => {
  const api = await createTestApi();
  const db = new pg.Client(DATABASE_URL);
  await db.connect();

  let runtimeId: string | null = null;
  let agentId: string | null = null;
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
         device_info, metadata, last_seen_at, owner_id
       )
       VALUES ($1, NULL, $2, 'cloud', 'kimi', 'online', $3, '{}'::jsonb, now(), $4)
       RETURNING id::text`,
      [workspace.id, `Kimi thinking ${Date.now()}`, "Kimi thinking E2E", userId],
    );
    runtimeId = runtime.rows[0]!.id;

    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (
         workspace_id, name, description, instructions, runtime_mode,
         runtime_config, runtime_id, visibility, max_concurrent_tasks,
         owner_id, model
       )
       VALUES ($1, 'Kimi Thinking Test Agent', '', '', 'cloud',
               '{}'::jsonb, $2, 'private', 1, $3, 'kimi-code/k3')
       RETURNING id::text`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;

    await page.route(`**/api/runtimes/${runtimeId}/models`, (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          id: "kimi-model-list",
          runtime_id: runtimeId,
          status: "completed",
          supported: true,
          models: [
            {
              id: "kimi-code/kimi-for-coding",
              label: "K2.7 Coding",
            },
            {
              id: "kimi-code/k3",
              label: "K3",
              default: true,
              thinking: {
                supported_levels: [
                  { value: "low", label: "Low" },
                  { value: "high", label: "High" },
                  { value: "max", label: "Max" },
                ],
                default_level: "high",
              },
            },
          ],
          created_at: "2026-08-04T00:00:00Z",
          updated_at: "2026-08-04T00:00:00Z",
        }),
      });
    });

    const token = api.getToken();
    if (!token) throw new Error("E2E token missing");
    await page.addInitScript((authToken) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);

    await page.goto(`/${workspace.slug}/agents/${agentId}?view=general`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "Kimi Thinking Test Agent");

    let trigger = page.getByRole("button", { name: /^Thinking ·/ });
    await expect(trigger).toBeVisible({ timeout: 15_000 });
    await expect(trigger).toContainText("Follow CLI config");
    await trigger.click();
    await page.getByText("High", { exact: true }).click();

    await expect
      .poll(async () => {
        const stored = await db.query<{ thinking_level: string | null }>(
          `SELECT thinking_level FROM agent WHERE id = $1`,
          [agentId],
        );
        return stored.rows[0]?.thinking_level;
      })
      .toBe("high");

    await page.reload({ waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Kimi Thinking Test Agent");
    trigger = page.getByRole("button", { name: /^Thinking ·/ });
    await expect(trigger).toContainText("High", { timeout: 15_000 });
  } finally {
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) {
      await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    }
    await db.end();
    await api.cleanup();
  }
});
