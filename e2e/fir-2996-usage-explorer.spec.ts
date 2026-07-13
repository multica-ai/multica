import { expect, test } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

const databaseUrl = process.env.DATABASE_URL!;

test("Dashboard shares filters, drills into runs, and persists a new visual", async ({ page }) => {
  const api = new TestApiClient();
  await api.login("fir-2996@multica.test", "FIR 2996");
  const workspace = await api.ensureWorkspace("Usage Explorer", "usage-explorer");
  await api.dismissStarterContent();
  const suffix = Date.now().toString(36);

  const db = new pg.Client(databaseUrl);
  await db.connect();
  try {
    await db.query(`DELETE FROM cerebro_analytics_visual WHERE workspace_id=$1`, [workspace.id]);
    await db.query(`DELETE FROM cerebro_analytics_run WHERE workspace_id=$1`, [workspace.id]);
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status)
       VALUES ($1,$2,'local','codex','online') RETURNING id`,
      [workspace.id, `Codex Runtime ${suffix}`],
    );
    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,status,runtime_id)
       VALUES ($1,$2,'local','{}','idle',$3) RETURNING id`,
      [workspace.id, `Lone ${suffix}`, runtime.rows[0].id],
    );
    const project = await db.query<{ id: string }>(
      `INSERT INTO project (workspace_id,title,status,priority) VALUES ($1,'Multica Runs','in_progress','medium') RETURNING id`,
      [workspace.id],
    );
    const issue = await api.createIssue(`Usage run ${suffix}`, {
      status: "done",
      project_id: project.rows[0].id,
    });
    const run = await db.query<{ id: string }>(
      `INSERT INTO agent_task_queue (agent_id,issue_id,runtime_id,status,priority,initiator_user_id,started_at,completed_at)
       VALUES ($1,$2,$3,'completed',0,$4,now()-interval '5 seconds',now()) RETURNING id`,
      [agent.rows[0].id, issue.id, runtime.rows[0].id, api.getUserId()],
    );
    await db.query(
      `INSERT INTO cerebro_analytics_run
       (workspace_id,run_id,population,source_type,source_id,source_label,person_id,person_label,agent_id,agent_label,project_id,project_label,runtime_id,runtime_label,provider,model,status,started_at,completed_at,duration_seconds,input_tokens,output_tokens,cost_cents,cost_kind,trace_id)
       VALUES ($1,$2,'agent','issue',$3,'Usage run',$4,'FIR 2996',$5,$8,$6,'Multica Runs',$7,$9,'openai','gpt-5','completed',now()-interval '5 seconds',now(),5,100,20,42,'actual',$10)`,
      [
        workspace.id,
        run.rows[0].id,
        issue.id,
        api.getUserId(),
        agent.rows[0].id,
        project.rows[0].id,
        runtime.rows[0].id,
        `Lone ${suffix}`,
        `Codex Runtime ${suffix}`,
        run.rows[0].id,
      ],
    );
    await db.query(
      `INSERT INTO cerebro_analytics_run_skill (analytics_run_id,workspace_id,skill_name,invocation_count,first_used_at,last_used_at)
       SELECT id,$1,'TDD',2,started_at,completed_at FROM cerebro_analytics_run WHERE run_id=$2`,
      [workspace.id, run.rows[0].id],
    );
    await db.query(
      `INSERT INTO cerebro_analytics_reference (workspace_id,analytics_run_id,reference_kind,reference_id,label,href)
       SELECT $1,id,'issue',$2,'Usage run',$3 FROM cerebro_analytics_run WHERE run_id=$4`,
      [workspace.id, issue.id, `/issues/${issue.id}?run=${run.rows[0].id}`, run.rows[0].id],
    );
    await db.query(
      `INSERT INTO cerebro_analytics_run_saving (analytics_run_id,workspace_id,saving_type,mode,applied,metric,baseline_value,effective_value,saved_tokens,saved_cents,measured_at)
       SELECT id,$1,'graphify','on',true,'context_tokens',100,60,40,7,now() FROM cerebro_analytics_run WHERE run_id=$2`,
      [workspace.id, run.rows[0].id],
    );
    await db.query(
      `INSERT INTO cerebro_analytics_quality_measurement (analytics_run_id,workspace_id,measurement_type,category,verdict,score,evaluator_version)
       SELECT id,$1,'evaluator','correctness','pass',1,'e2e' FROM cerebro_analytics_run WHERE run_id=$2`,
      [workspace.id, run.rows[0].id],
    );

    const token = api.getToken()!;
    await page.addInitScript((value) => localStorage.setItem("multica_token", value), token);
    await page.goto(`/${workspace.slug}/dashboard`);

    await expect(page.getByRole("heading", { name: "Activity", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "People and projects" })).toBeVisible();
    await page.getByRole("button", { name: "Include provider openai" }).click();
    await expect(page).toHaveURL(/provider=openai/);

    await page.getByRole("button", { name: "Runs", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Matching runs" })).toBeVisible();
    // Click the run row (plain context cell) to open the debug/trace rail.
    await page.getByRole("cell", { name: "Usage run", exact: true }).first().click();
    await expect(page.getByRole("complementary", { name: "Run and debug context" })).toBeVisible();
    await expect(page.getByText(run.rows[0].id, { exact: true }).first()).toBeVisible();
    await expect(page.locator(`aside a[href*="${issue.id}"]`)).toBeVisible();

    await page.getByRole("button", { name: "New visual" }).click();
    await page.getByLabel("Visual title").fill("Models by cost");
    await page.getByLabel("Metric").selectOption("cost_cents");
    await page.getByLabel("Dimension").selectOption("model");
    await page.getByRole("button", { name: "Create visual" }).click();
    await expect(page.getByRole("heading", { name: "Models by cost" })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("heading", { name: "Models by cost" })).toBeVisible();
  } finally {
    await db.query(`DELETE FROM cerebro_analytics_visual WHERE workspace_id=$1`, [workspace.id]).catch(() => {});
    await db.end();
  }
});
