import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

test("agent page edits identity and instructions and starts a message", async ({
  page,
}) => {
  const slug = await loginAsDefault(page);
  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);
  await api.setWorkspaceFeatureFlag("cerebro_agent_page_redesign", true);

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       ) VALUES ($1, NULL, $2, 'cloud', 'e2e_agent_page', 'online', $3, '{}'::jsonb, now())
       RETURNING id`,
      [workspace.id, `FIR-2670 runtime ${Date.now()}`, "FIR-2670 runtime"],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, description, instructions, runtime_mode,
         runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, $3, $4, 'cloud', '{}'::jsonb, $5, 'workspace', 1, $6)
       RETURNING id`,
      [
        workspace.id,
        `FIR-2670 Agent ${Date.now()}`,
        "Original description",
        "Original instructions",
        runtimeId,
        userId,
      ],
    )
  ).rows[0].id as string;

  try {
    await page.goto(`/${slug}/agents/${agentId}`, {
      waitUntil: "domcontentloaded",
    });

    await page.getByRole("button", { name: "Edit agent name" }).click();
    const nameInput = page.getByRole("textbox", { name: "agent name" });
    await nameInput.fill("Edited FIR-2670 Agent");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Edited FIR-2670 Agent", { exact: true }).first()).toBeVisible();

    await page.getByRole("button", { name: "Edit agent description" }).click();
    await page
      .getByRole("textbox", { name: "agent description" })
      .fill("Edited description");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Edited description", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Instructions" }).click();
    const instructions = page.getByRole("textbox", { name: "Instructions" });
    await instructions.fill("Edited instructions");
    await page.getByLabel("Change title").fill("Update operating instructions");
    await page.getByRole("button", { name: "Save & approve" }).click();
    await expect(page.getByText(/Change approved/)).toBeVisible();
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Instructions" }).click();
    await expect(page.getByRole("textbox", { name: "Instructions" })).toHaveValue(
      "Edited instructions",
    );

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole("button", { name: "Message", exact: true })).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Instructions" })).toBeVisible();

    await page.getByRole("button", { name: "Message", exact: true }).click();
    await expect(page).toHaveURL(
      new RegExp(`/inbox\\?chat=new-chat&agent=${agentId}$`),
    );
    await expect(page.getByRole("heading", { name: "Edited FIR-2670 Agent" })).toBeVisible();
    await expect(page.getByText("Start a conversation with Edited FIR-2670 Agent")).toBeVisible();
  } finally {
    await api.setWorkspaceFeatureFlag("cerebro_agent_page_redesign", false);
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.end();
    await api.cleanup();
  }
});
