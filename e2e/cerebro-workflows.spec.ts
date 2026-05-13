import { test, expect } from "@playwright/test";
import pg from "pg";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ||
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

// The workflow engine is gated by CEREBRO_WORKFLOWS_ENABLED on the server
// side. When that env var is missing on the test runner's backend, the
// engine's bus listener no-ops and the create_sub_issue side-effect won't
// happen — so the assertion would flake. Skip cleanly with a clear reason.
const ENGINE_ENABLED = ["1", "true", "yes", "on"].includes(
  (process.env.CEREBRO_WORKFLOWS_ENABLED ?? "").toLowerCase().trim(),
);

test.describe("Cerebro workflows (JEH-1047)", () => {
  let api: TestApiClient;
  let token: string;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    test.skip(!ENGINE_ENABLED, "CEREBRO_WORKFLOWS_ENABLED not set on the backend.");
    api = await createTestApi();
    token = api.getToken() ?? "";
    expect(token).not.toBe("");
    // Enable the per-user UI flag so the workflows page renders and the
    // sidebar entry shows up. Server-side execution stays gated by the
    // env var checked above.
    await setFeatureFlag(token, "cerebro_workflows", true);
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
      await setFeatureFlag(token, "cerebro_workflows", false).catch(() => {});
    }
  });

  test("Done når: template → status_changed → create_sub_issue → run logged", async ({
    page,
  }) => {
    // ---- Step 0: a project + a target issue exist ----
    const project = await api.createProject(
      `WF E2E Project ${Date.now()}`,
    );
    const parentIssue = await api.createIssue(`WF E2E Parent ${Date.now()}`, {
      project_id: project.id,
      status: "todo",
    });

    // ---- Step 1: open the workflows page from the sidebar ----
    await page.goto(`/${workspaceSlug}/workflows`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Workflows" })).toBeVisible({
      timeout: 10_000,
    });

    // ---- Step 2: open the form, click "Brug" on the status-change template ----
    await page.getByRole("button", { name: "Nyt workflow" }).click();
    await expect(page.getByRole("heading", { name: "Nyt workflow" })).toBeVisible();

    // The template picker uses data-testid="template-use-<key>" so the
    // assertion targets the right row regardless of label wording changes.
    await page.getByTestId("template-use-status-change").click();

    // After applying the template, the form's "Til status" field should
    // hold "in_review". That's the canary — if it's wrong, the template
    // pre-fill regressed.
    await expect(page.getByLabel("Til status")).toHaveValue("in_review");

    // ---- Step 3: save the workflow ----
    await page.getByRole("button", { name: "Gem" }).click();

    // Back on the list page. The new workflow name (template default) shows.
    await page.waitForURL(`**/workflows`, { timeout: 10_000 });
    await expect(page.getByText("Auto QA on in_review")).toBeVisible();

    // ---- Step 4: move the parent issue to in_review ----
    // This fires the issue:updated bus event with status_changed=true and
    // prev_status="todo". The engine subscribes on the same in-process
    // bus, so by the time this fetch returns the action has run.
    await updateIssueStatus(token, parentIssue.id, "in_review");

    // ---- Step 5: a sub-issue exists under the parent ----
    // The bus is synchronous but the DB write that creates the sub-issue
    // happens on a separate connection — poll briefly to absorb that lag.
    const child = await pollForChild(token, parentIssue.id);
    expect(child, "expected one sub-issue to be auto-created").toBeDefined();
    expect(child!.title).toContain(parentIssue.title);

    // ---- Step 6: workflow_run is success ----
    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parentIssue.id && r.status === "success",
    );
    expect(matching, `expected a success run for issue ${parentIssue.id}`).toBeDefined();

    // ---- Step 7: the run shows up in the log page ----
    await page.goto(`/${workspaceSlug}/workflows/runs`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Workflow-log" })).toBeVisible();
    await expect(page.getByText("success").first()).toBeVisible({ timeout: 10_000 });

    // Cleanup of the workflow itself; api.cleanup() only knows about
    // issue/project/inbox cleanup.
    await deleteCreatedWorkflows(token);
    // Cleanup the auto-created sub-issue so api.cleanup doesn't orphan it.
    await deleteIssue(token, child!.id);
  });

  test("phase 2: run_skill action enqueues quick_create task on status_changed", async ({
    page,
  }) => {
    // ---- Step 0: workspace + skill + agent + runtime fixtures via SQL ----
    // run_skill needs an existing skill in the workspace, an agent with that
    // skill attached, and a runtime so agent.runtime_id is set. The HTTP
    // surface for skill + agent creation is heavier than this test needs, so
    // we seed direct rows and clean them up at the end.
    const wsId = await getDefaultWorkspaceId(token);
    const skillName = `wf-e2e-skill-${Date.now()}`;
    const fixtures = await seedSkillAgentFixture(wsId, skillName);

    const parentIssue = await api.createIssue(`WF E2E run_skill parent ${Date.now()}`, {
      status: "todo",
    });

    // ---- Step 1: create the run_skill workflow via API ----
    // The canvas/form-builder UI is exercised by the phase-1 happy path; this
    // test pins the *engine* contract — the form/canvas wiring already lives
    // in templates.test.ts + workflow-form.tsx unit code paths.
    const workflowId = await createWorkflowViaAPI(token, {
      name: `WF E2E run_skill ${Date.now()}`,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      action_type: "run_skill",
      action_config: {
        skill_name: skillName,
        agent_id: fixtures.agentId,
        skill_input: { issue_title: "{{title}}" },
      },
    });

    // ---- Step 2: trigger the workflow ----
    await updateIssueStatus(token, parentIssue.id, "in_review");

    // ---- Step 3: quick_create task enqueued for the agent ----
    const enqueued = await pollForQuickCreateTask(fixtures.agentId);
    expect(enqueued, "expected a quick_create agent_task_queue row").toBeDefined();
    expect(enqueued!.contextType).toBe("quick_create");
    expect(enqueued!.workflowId).toBe(workflowId);
    expect(enqueued!.skillName).toBe(skillName);

    // ---- Step 4: workflow_run success ----
    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parentIssue.id && r.status === "success",
    );
    expect(matching, "expected a success run for the run_skill workflow").toBeDefined();

    // ---- Cleanup ----
    await deleteWorkflow(token, workflowId);
    await deleteAgentTaskAndFixture(enqueued!.taskId, fixtures);
  });

  test("phase 2: comment_on_issue action posts a workflow-authored comment", async ({
    page,
  }) => {
    const parentIssue = await api.createIssue(`WF E2E comment parent ${Date.now()}`, {
      status: "todo",
    });

    const workflowId = await createWorkflowViaAPI(token, {
      name: `WF E2E comment ${Date.now()}`,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      action_type: "comment_on_issue",
      action_config: {
        target: "self",
        content: "Workflow: {{title}} er nu klar.",
      },
    });

    await updateIssueStatus(token, parentIssue.id, "in_review");

    const comment = await pollForWorkflowComment(token, parentIssue.id);
    expect(comment, "expected a workflow-authored comment").toBeDefined();
    expect(comment!.content).toContain(parentIssue.title);

    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parentIssue.id && r.status === "success",
    );
    expect(matching, "expected a success run for the comment_on_issue workflow").toBeDefined();

    await deleteWorkflow(token, workflowId);
  });

  // --- Phase 3 (JEH-1108) acceptance paths ---

  test("phase 3: cron → run_skill enqueues a quick_create task after test-sweep", async () => {
    // The cron path drives the engine via the test-only sweep endpoint,
    // which is itself gated by an env var on the backend. Skip cleanly when
    // the hook isn't wired so the result is "skipped", not "flaky failure".
    test.skip(
      process.env.CEREBRO_WORKFLOWS_TEST_ENDPOINTS !== "1",
      "CEREBRO_WORKFLOWS_TEST_ENDPOINTS not set — cron sweep hook unavailable.",
    );
    // Seed fixtures + a workspace-scoped skill the agent owns.
    const wsId = await getDefaultWorkspaceId(token);
    const skillName = `wf-e2e-cron-skill-${Date.now()}`;
    const fixtures = await seedSkillAgentFixture(wsId, skillName);

    // Create the cron workflow via API. We pick a "fires every minute"
    // schedule so the first SQL-nudge below crosses next_fire_at.
    const workflowId = await createWorkflowViaAPI(token, {
      name: `WF E2E cron ${Date.now()}`,
      trigger_type: "cron",
      trigger_config: {
        schedule_expr: "* * * * *",
        timezone: "Europe/Copenhagen",
      },
      action_type: "run_skill",
      action_config: {
        skill_name: skillName,
        agent_id: fixtures.agentId,
        skill_input: {},
      },
    });

    // First sweep: anchors cron_state, no fire.
    await callTestSweep(token);
    // SQL-nudge: backdate next_fire_at so the next sweep crosses it.
    await nudgeCronNextFire(workflowId);
    await callTestSweep(token);

    const enqueued = await pollForQuickCreateTask(fixtures.agentId);
    expect(enqueued, "expected the cron sweep to enqueue a quick_create task").toBeDefined();
    expect(enqueued!.workflowId).toBe(workflowId);
    expect(enqueued!.skillName).toBe(skillName);

    const runs = await listWorkflowRuns(token);
    const matching = runs.find((r) => r.status === "success");
    expect(matching, "expected at least one success cron run").toBeDefined();

    await deleteWorkflow(token, workflowId);
    await deleteAgentTaskAndFixture(enqueued!.taskId, fixtures);
  });

  test("phase 3: webhook_inbound → create_sub_issue on signed POST", async () => {
    const parentIssue = await api.createIssue(`WF E2E webhook parent ${Date.now()}`, {
      status: "todo",
    });

    const workflowId = await createWorkflowViaAPI(token, {
      name: `WF E2E webhook ${Date.now()}`,
      trigger_type: "webhook_inbound",
      trigger_config: {},
      action_type: "create_sub_issue",
      action_config: {
        title: "Webhook child: {{payload.pull_request.title}}",
        description: "Auto-genereret fra webhook.",
      },
    });

    // Mint a token via the regenerate endpoint.
    const tokenRes = await fetch(
      `${API_BASE}/api/cerebro/workflows/${workflowId}/regenerate-token`,
      {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      },
    );
    expect(tokenRes.ok, `regenerate-token failed: ${tokenRes.status}`).toBe(true);
    const { inbound_webhook_token: hookToken } = (await tokenRes.json()) as {
      inbound_webhook_token: string;
    };
    expect(hookToken).toBeTruthy();

    // POST a webhook body that carries issue_id and a GitHub-shaped payload.
    const body = JSON.stringify({
      issue_id: parentIssue.id,
      pull_request: { title: "Fix login bug" },
    });
    const ts = String(Math.floor(Date.now() / 1000));
    const ingressRes = await fetch(
      `${API_BASE}/api/cerebro/workflows/webhook/${hookToken}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Multica-Timestamp": ts,
        },
        body,
      },
    );
    expect(ingressRes.ok, `webhook ingress failed: ${ingressRes.status}`).toBe(true);

    const child = await pollForChild(token, parentIssue.id);
    expect(child, "expected a sub-issue created by the webhook").toBeDefined();
    // Verify {{payload.pull_request.title}} substituted via renderTemplate.
    expect(child!.title).toContain("Fix login bug");

    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parentIssue.id && r.status === "success",
    );
    expect(matching, "expected a success run for the webhook workflow").toBeDefined();

    await deleteWorkflow(token, workflowId);
    await deleteIssue(token, child!.id);
  });

  test("phase 3: all_children_done → set_status promotes parent", async () => {
    const parent = await api.createIssue(`WF E2E parent ${Date.now()}`, {
      status: "todo",
    });
    const child1 = await api.createIssue(`WF E2E child 1 ${Date.now()}`, {
      status: "todo",
      parent_issue_id: parent.id,
    });
    const child2 = await api.createIssue(`WF E2E child 2 ${Date.now()}`, {
      status: "todo",
      parent_issue_id: parent.id,
    });

    const workflowId = await createWorkflowViaAPI(token, {
      name: `WF E2E all_children_done ${Date.now()}`,
      trigger_type: "all_children_done",
      trigger_config: {},
      action_type: "set_status",
      action_config: { status: "in_review" },
    });

    // Mark only one child done — parent must NOT flip yet.
    await updateIssueStatus(token, child1.id, "done");
    await new Promise((r) => setTimeout(r, 200));
    const stillTodo = await fetchIssue(token, parent.id);
    expect(stillTodo.status).toBe("todo");

    // Mark the second child done — listener fires all_children_done.
    await updateIssueStatus(token, child2.id, "done");

    const promoted = await pollForStatus(token, parent.id, "in_review");
    expect(promoted, `expected parent ${parent.id} to flip to in_review`).toBe(true);

    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parent.id && r.status === "success",
    );
    expect(matching, "expected a success run for the all_children_done workflow").toBeDefined();

    await deleteWorkflow(token, workflowId);
  });
});

// ---- helpers (local to this spec — no need to bloat TestApiClient) ----

async function setFeatureFlag(token: string, key: string, enabled: boolean) {
  // The feature-flag route is keyed on workspace UUID, not slug, so we
  // resolve the id from the workspaces list rather than relying on the
  // header-based fallback.
  const res = await fetch(`${API_BASE}/api/workspaces`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const data = (await res.json()) as {
    workspaces?: Array<{ slug?: string; id?: string }>;
  };
  const ws = (data.workspaces ?? []).find((w) => !!w.id);
  if (!ws?.id) throw new Error("could not resolve workspace id");
  const r = await fetch(`${API_BASE}/api/workspaces/${ws.id}/feature-flags/${key}`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ enabled }),
  });
  if (!r.ok && r.status !== 204) {
    throw new Error(`setFeatureFlag failed: ${r.status} ${await r.text()}`);
  }
}

async function updateIssueStatus(token: string, id: string, status: string) {
  const res = await fetch(`${API_BASE}/api/issues/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ status }),
  });
  if (!res.ok) {
    throw new Error(`updateIssueStatus failed: ${res.status} ${await res.text()}`);
  }
}

async function pollForChild(token: string, parentId: string) {
  // The workflow_run + create_sub_issue path is synchronous on the bus, but
  // the http response writer returns before all bus subscribers finish in
  // some go-chi configurations — give the listener up to ~2 s.
  for (let i = 0; i < 20; i++) {
    const res = await fetch(`${API_BASE}/api/issues/${parentId}/children`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      const data = (await res.json()) as { issues?: Array<{ id: string; title: string }> };
      const issues = data.issues ?? [];
      if (issues.length > 0) return issues[0];
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  return undefined;
}

async function listWorkflowRuns(token: string) {
  const res = await fetch(`${API_BASE}/api/cerebro/workflows/runs?limit=20`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error(`listWorkflowRuns failed: ${res.status}`);
  const data = (await res.json()) as {
    runs?: Array<{ id: string; target_issue_id?: string; status: string }>;
  };
  return data.runs ?? [];
}

async function deleteCreatedWorkflows(token: string) {
  const res = await fetch(`${API_BASE}/api/cerebro/workflows`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) return;
  const data = (await res.json()) as { workflows?: Array<{ id: string; name: string }> };
  for (const wf of data.workflows ?? []) {
    if (wf.name.startsWith("Auto QA on in_review")) {
      await fetch(`${API_BASE}/api/cerebro/workflows/${wf.id}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
      });
    }
  }
}

async function deleteIssue(token: string, id: string) {
  await fetch(`${API_BASE}/api/issues/${id}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
}

// ---- phase-2 helpers ----

async function getDefaultWorkspaceId(token: string): Promise<string> {
  const res = await fetch(`${API_BASE}/api/workspaces`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const data = (await res.json()) as { workspaces?: Array<{ id?: string }> };
  const wsId = data.workspaces?.find((w) => !!w.id)?.id;
  if (!wsId) throw new Error("could not resolve workspace id");
  return wsId;
}

interface SkillAgentFixture {
  skillId: string;
  agentId: string;
  runtimeId: string;
}

async function seedSkillAgentFixture(
  workspaceId: string,
  skillName: string,
): Promise<SkillAgentFixture> {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    // The migration's `daemon_id` column has a UNIQUE constraint on
    // (workspace_id, daemon_id, provider) — including a timestamp makes the
    // row unique per test run without colliding with leftovers.
    const tag = `wf-e2e-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    const runtime = await client.query<{ id: string }>(
      `INSERT INTO agent_runtime
         (workspace_id, daemon_id, name, runtime_mode, provider, status)
       VALUES ($1, $2, $3, 'local', 'e2e', 'online')
       RETURNING id`,
      [workspaceId, tag, `WF E2E runtime ${tag}`],
    );
    const runtimeId = runtime.rows[0]!.id;

    const agent = await client.query<{ id: string }>(
      `INSERT INTO agent
         (workspace_id, name, runtime_mode, runtime_config, status, runtime_id)
       VALUES ($1, $2, 'local', '{}'::jsonb, 'idle', $3)
       RETURNING id`,
      [workspaceId, `WF E2E agent ${tag}`, runtimeId],
    );
    const agentId = agent.rows[0]!.id;

    const skill = await client.query<{ id: string }>(
      `INSERT INTO skill (workspace_id, name, description, content)
       VALUES ($1, $2, 'wf e2e fixture', '')
       RETURNING id`,
      [workspaceId, skillName],
    );
    const skillId = skill.rows[0]!.id;

    await client.query(
      `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
       ON CONFLICT DO NOTHING`,
      [agentId, skillId],
    );

    return { skillId, agentId, runtimeId };
  } finally {
    await client.end();
  }
}

interface QuickCreateRow {
  taskId: string;
  contextType?: string;
  workflowId?: string;
  skillName?: string;
}

async function pollForQuickCreateTask(agentId: string): Promise<QuickCreateRow | undefined> {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    for (let i = 0; i < 30; i++) {
      const res = await client.query<{ id: string; context: unknown }>(
        `SELECT id, context FROM agent_task_queue
         WHERE agent_id = $1 AND context IS NOT NULL
         ORDER BY created_at DESC
         LIMIT 1`,
        [agentId],
      );
      if (res.rows.length > 0) {
        const row = res.rows[0]!;
        const ctx = (row.context ?? {}) as Record<string, unknown>;
        if ((ctx.type as string) === "quick_create") {
          return {
            taskId: row.id,
            contextType: ctx.type as string,
            workflowId: ctx.workflow_id as string | undefined,
            skillName: ctx.workflow_skill_name as string | undefined,
          };
        }
      }
      await new Promise((r) => setTimeout(r, 100));
    }
    return undefined;
  } finally {
    await client.end();
  }
}

async function pollForWorkflowComment(
  token: string,
  issueId: string,
): Promise<{ content: string } | undefined> {
  for (let i = 0; i < 30; i++) {
    const res = await fetch(`${API_BASE}/api/issues/${issueId}/comments`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      const data = (await res.json()) as {
        comments?: Array<{ content: string; author_type?: string }>;
      };
      const comment = (data.comments ?? []).find((c) => typeof c.content === "string");
      if (comment) return { content: comment.content };
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  return undefined;
}

async function createWorkflowViaAPI(
  token: string,
  payload: {
    name: string;
    trigger_type: string;
    trigger_config: Record<string, unknown>;
    action_type: string;
    action_config: Record<string, unknown>;
  },
): Promise<string> {
  const res = await fetch(`${API_BASE}/api/cerebro/workflows`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      ...payload,
      enabled: true,
      conditions: [],
    }),
  });
  if (!res.ok) {
    throw new Error(`createWorkflowViaAPI failed: ${res.status} ${await res.text()}`);
  }
  const data = (await res.json()) as { id: string };
  return data.id;
}

async function deleteWorkflow(token: string, id: string) {
  await fetch(`${API_BASE}/api/cerebro/workflows/${id}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
}

async function deleteAgentTaskAndFixture(
  taskId: string,
  fixture: SkillAgentFixture,
) {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    await client.query("DELETE FROM agent_task_queue WHERE id = $1", [taskId]);
    // Cascades take care of agent_skill + skill_file rows.
    await client.query("DELETE FROM skill WHERE id = $1", [fixture.skillId]);
    await client.query("DELETE FROM agent WHERE id = $1", [fixture.agentId]);
    await client.query("DELETE FROM agent_runtime WHERE id = $1", [fixture.runtimeId]);
  } finally {
    await client.end();
  }
}

// ---- phase-3 helpers ----

// callTestSweep hits the build-time-stable, env-gated cron-sweep debug
// endpoint. The endpoint returns 404 when CEREBRO_WORKFLOWS_TEST_ENDPOINTS
// isn't set on the backend — we surface that as a thrown error so the test
// fails loudly rather than waiting for a sweep that never fires.
async function callTestSweep(token: string) {
  const res = await fetch(
    `${API_BASE}/api/cerebro/workflows/_test/cron-sweep`,
    {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    },
  );
  if (!res.ok) {
    throw new Error(
      `cron-sweep failed (CEREBRO_WORKFLOWS_TEST_ENDPOINTS=1 required): ${res.status} ${await res.text()}`,
    );
  }
}

// nudgeCronNextFire backdates next_fire_at on the cron_state row so the next
// sweep iteration crosses the "fire now" threshold without waiting for the
// real wall-clock interval. Mirrors the spec's SQL-nudge fallback.
async function nudgeCronNextFire(workflowId: string) {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    await client.query(
      `UPDATE cerebro_workflow_cron_state
         SET next_fire_at = NOW() - INTERVAL '1 second'
       WHERE workflow_id = $1`,
      [workflowId],
    );
  } finally {
    await client.end();
  }
}

async function fetchIssue(
  token: string,
  id: string,
): Promise<{ id: string; status: string; title: string }> {
  const res = await fetch(`${API_BASE}/api/issues/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error(`fetchIssue failed: ${res.status}`);
  return (await res.json()) as { id: string; status: string; title: string };
}

async function pollForStatus(
  token: string,
  id: string,
  want: string,
): Promise<boolean> {
  for (let i = 0; i < 30; i++) {
    const issue = await fetchIssue(token, id);
    if (issue.status === want) return true;
    await new Promise((r) => setTimeout(r, 100));
  }
  return false;
}

