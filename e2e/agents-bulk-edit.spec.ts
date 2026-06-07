import "./env";
import { test, expect } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";
import { gotoAppPage } from "./helpers";

const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

interface SeededAgent {
  id: string;
  name: string;
}

test.describe("Agents bulk edit", () => {
  test("updates selected agents through the bulk edit dialog", async ({ page }, testInfo) => {
    test.setTimeout(60000);

    const suffix = `${Date.now()}-${testInfo.parallelIndex}`;
    const api = new TestApiClient();
    const pgClient = new pg.Client(DATABASE_URL);
    const createdAgentIds: string[] = [];
    let runtimeId: string | null = null;

    await api.login(`bulk-edit-${suffix}@localhost`, "Bulk Edit Tester");
    const workspace = await api.ensureWorkspace(
      "E2E Bulk Edit Workspace",
      `e2e-bulk-edit-${suffix}`,
    );
    await api.completeOnboarding();
    await pgClient.connect();

    try {
      const owner = await pgClient.query<{ user_id: string }>(
        `SELECT user_id FROM member WHERE workspace_id = $1 AND role = 'owner' LIMIT 1`,
        [workspace.id],
      );
      if (owner.rows.length === 0) throw new Error("workspace owner missing");

      const runtime = await pgClient.query<{ id: string }>(
        `INSERT INTO agent_runtime (
           workspace_id, daemon_id, name, runtime_mode, provider, status,
           device_info, metadata, last_seen_at
         )
         VALUES ($1, NULL, $2, 'cloud', 'codex', 'online', 'E2E bulk runtime', '{}'::jsonb, now())
         RETURNING id`,
        [workspace.id, `E2E Bulk Runtime ${suffix}`],
      );
      runtimeId = runtime.rows[0]!.id;

      const seededAgents: SeededAgent[] = [];
      for (const label of ["A", "B", "C"]) {
        const inserted = await pgClient.query<{ id: string; name: string }>(
          `INSERT INTO agent (
             workspace_id, name, description, runtime_mode, runtime_config,
             runtime_id, visibility, max_concurrent_tasks, owner_id,
             custom_args, custom_env, model
           )
           VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4,
                   '["--old"]'::jsonb, '{"KEEP":"old-secret"}'::jsonb, 'old-model')
           RETURNING id, name`,
          [
            workspace.id,
            `E2E Bulk Agent ${label} ${suffix}`,
            runtimeId,
            owner.rows[0]!.user_id,
          ],
        );
        const agent = inserted.rows[0]!;
        seededAgents.push(agent);
        createdAgentIds.push(agent.id);
      }

      const token = api.getToken();
      if (!token) throw new Error("test api client not logged in");
      await page.addInitScript((t) => {
        localStorage.setItem("multica_token", t);
        localStorage.setItem("multica:chat:isOpen", "false");
      }, token);

      await gotoAppPage(page, `/${workspace.slug}/agents`);
      await expect(page.getByRole("heading", { name: "Agents", exact: true })).toBeVisible({
        timeout: 15000,
      });
      try {
        await page.waitForLoadState("load", { timeout: 5000 });
      } catch {
        await page.evaluate(() => window.stop());
      }
      const agentsTable = page.getByRole("table");
      for (const agent of seededAgents) {
        await expect(
          agentsTable.getByRole("checkbox", { name: `Select ${agent.name}` }),
        ).toBeVisible({ timeout: 15000 });
      }

      const firstAgentCheckbox = agentsTable.getByRole("checkbox", {
        name: `Select ${seededAgents[0]!.name}`,
      });
      const secondAgentCheckbox = agentsTable.getByRole("checkbox", {
        name: `Select ${seededAgents[1]!.name}`,
      });
      await firstAgentCheckbox.click();
      await expect(firstAgentCheckbox).toBeChecked();
      await secondAgentCheckbox.click();
      await expect(secondAgentCheckbox).toBeChecked();
      await expect(page.getByText("2 agents selected")).toBeVisible();
      await page.getByRole("button", { name: "Bulk edit selected" }).click();

      const dialog = page.getByRole("dialog", { name: "Bulk edit selected agents" });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByText("This will update 2 agents.")).toBeVisible();

      await dialog.getByRole("checkbox", { name: "Model" }).click();
      await dialog.getByRole("textbox", { name: "Model" }).fill("gpt-5.5");

      await dialog.getByRole("checkbox", { name: "Custom args" }).click();
      const customArgInput = dialog.getByLabel("Custom arg to add");
      await expect(customArgInput).toBeVisible();
      await customArgInput.fill("--max-turns 100");

      await dialog.getByRole("checkbox", { name: "Environment variables" }).click();
      await dialog.getByLabel("Set env key").fill("BULK_E2E_KEY");
      await dialog.getByRole("textbox", { name: "Set env value" }).fill("bulk-secret");

      await dialog.getByRole("button", { name: "Apply" }).click();
      await expect(page.getByText("Updated 2 agents")).toBeVisible({ timeout: 15000 });

      await expect
        .poll(async () => {
          const rows = await pgClient.query<{
            id: string;
            model: string;
            custom_args: string[];
            custom_env: Record<string, string>;
          }>(
            `SELECT id, model, custom_args, custom_env
             FROM agent
             WHERE id = ANY($1::uuid[])
             ORDER BY name ASC`,
            [seededAgents.map((agent) => agent.id)],
          );
          return rows.rows.map((row) => ({
            id: row.id,
            model: row.model,
            customArgs: row.custom_args,
            customEnv: row.custom_env,
          }));
        })
        .toEqual([
          {
            id: seededAgents[0]!.id,
            model: "gpt-5.5",
            customArgs: ["--old", "--max-turns", "100"],
            customEnv: { KEEP: "old-secret", BULK_E2E_KEY: "bulk-secret" },
          },
          {
            id: seededAgents[1]!.id,
            model: "gpt-5.5",
            customArgs: ["--old", "--max-turns", "100"],
            customEnv: { KEEP: "old-secret", BULK_E2E_KEY: "bulk-secret" },
          },
          {
            id: seededAgents[2]!.id,
            model: "old-model",
            customArgs: ["--old"],
            customEnv: { KEEP: "old-secret" },
          },
        ]);
    } finally {
      if (createdAgentIds.length > 0) {
        await pgClient.query(`DELETE FROM agent WHERE id = ANY($1::uuid[])`, [createdAgentIds]);
      }
      if (runtimeId) {
        await pgClient.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
      }
      await pgClient.end();
    }
  });
});
