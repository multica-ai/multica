import { expect, test } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

const databaseUrl = process.env.DATABASE_URL!;

test("Usage explorer filters every result and opens a Multica run", async ({ page }) => {
  const api = new TestApiClient();
  await api.login("fir-2996@multica.test", "FIR 2996");
  const workspace = await api.ensureWorkspace("Usage Explorer", "usage-explorer");
  await api.dismissStarterContent();

  const db = new pg.Client(databaseUrl);
  await db.connect();
  try {
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status)
       VALUES ($1,'Codex Runtime','local','codex','online') RETURNING id`,
      [workspace.id],
    );
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,status,runtime_id)
       VALUES ($1,'Lone','local','{}','idle',$2) RETURNING id`,
      [workspace.id, runtime.rows[0].id],
    );
    const project = await db.query<{ id: string }>(
      `INSERT INTO project (workspace_id,title,status,priority) VALUES ($1,'Multica Runs','in_progress','medium') RETURNING id`,
      [workspace.id],
    );
    const issue = await db.query<{ id: string }>(
      `INSERT INTO issue (workspace_id,title,status,creator_type,creator_id,project_id)
       VALUES ($1,'Usage run','done','member',$2,$3) RETURNING id`,
      [workspace.id, api.getUserId(), project.rows[0].id],
    );
    const run = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id,issue_id,runtime_id,status,priority,started_at,completed_at)
       VALUES ($1,$2,$3,'completed',0,now()-interval '5 seconds',now()) RETURNING id`,
      [agent.rows[0].id, issue.rows[0].id, runtime.rows[0].id],
    );
    await db.query(
      `INSERT INTO task_usage (task_id,provider,model,input_tokens,output_tokens,cost_cents)
       VALUES ($1,'openai','gpt-5',100,20,42)`,
      [run.rows[0].id],
    );
    await db.query(
      `INSERT INTO cerebro_cost_optimization_measurement
       (workspace_id,task_id,saving_key,mode,applied,metric,baseline_value,effective_value,saved_cents)
       VALUES ($1,$2,'graphify','on',true,'context_tokens',100,60,7)`,
      [workspace.id, run.rows[0].id],
    );

    const token = api.getToken()!;
    await page.addInitScript((value) => localStorage.setItem("multica_token", value), token);
    await page.goto(`/${workspace.slug}/dashboard`);

    await expect(page.getByRole("heading", { name: "Filter every result" })).toBeVisible();
    await expect(page.getByText("$0.42")).toBeVisible();
    await expect(page.getByText("graphify", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Open run " + run.rows[0].id })).toBeVisible();

    await page.getByRole("button", { name: "Include model gpt-5" }).click();
    await expect(page).toHaveURL(/model=gpt-5/);
    await expect(page.getByText("1 total")).toBeVisible();

    await page.getByRole("button", { name: "Open run " + run.rows[0].id }).click();
    await expect(page.getByRole("heading", { name: "Run details" })).toBeVisible();
    await expect(page.getByText("Multica Runs")).toBeVisible();
  } finally {
    await db.end();
  }
});
