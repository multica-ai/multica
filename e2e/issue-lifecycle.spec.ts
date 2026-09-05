import { test, expect, type Page, type TestInfo } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

interface LifecycleStatus {
  id: string;
  legacy_status_key: string | null;
  name: string;
}

interface LifecycleResponse {
  lifecycle: { id: string; revision: number };
  statuses: LifecycleStatus[];
  mode: "default" | "custom";
}

interface IssueResponse {
  id: string;
  title: string;
  status: string;
  revision: number;
  transition_id: string | null;
  lifecycle_id: string;
  lifecycle_status_id: string;
  assignee_type: string | null;
  assignee_id: string | null;
}

interface AutomationExecution {
  id: string;
  status_id: string;
  status: string;
  executor_type: string | null;
  executor_id: string | null;
  policy_snapshot: { instructions: string };
}

async function request<T>(
  api: TestApiClient,
  workspaceId: string,
  path: string,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${api.getToken()}`,
      "X-Workspace-ID": workspaceId,
      ...((init?.headers as Record<string, string>) ?? {}),
    },
  });
  if (!response.ok) {
    throw new Error(`${init?.method ?? "GET"} ${path} failed: ${response.status} ${await response.text()}`);
  }
  return response.json() as Promise<T>;
}

async function updateStatus(
  api: TestApiClient,
  workspaceId: string,
  lifecycle: LifecycleResponse,
  legacyKey: string,
  body: Record<string, unknown>,
) {
  const status = lifecycle.statuses.find((candidate) => candidate.legacy_status_key === legacyKey);
  if (!status) throw new Error(`Missing ${legacyKey} lifecycle status`);
  return request<LifecycleResponse>(api, workspaceId, `/api/issue-lifecycles/${lifecycle.lifecycle.id}/statuses/${status.id}`, {
    method: "PATCH",
    body: JSON.stringify({ expected_revision: lifecycle.lifecycle.revision, ...body }),
  });
}

async function chooseStatus(page: Page, current: string, next: string) {
  await page.getByRole("button", { name: current, exact: true }).first().click();
  const option = page.locator("button[data-picker-item]", { hasText: next });
  await expect(option).toBeVisible();
  await option.click();
  await expect(page.getByRole("button", { name: next, exact: true }).first()).toBeVisible();
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const path = testInfo.outputPath(name);
  await page.screenshot({ path, fullPage: true });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

test.describe("project-scoped issue lifecycle", () => {
  let api: TestApiClient;
  let workspaceId = "";
  let projectId = "";
  let agentId = "";
  let runtimeId = "";
  let squadId = "";

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const workspace = (await api.getWorkspaces())[0];
    if (!workspace) throw new Error("E2E workspace was not created");
    workspaceId = workspace.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
    if (!workspaceId) return;
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      if (projectId) await client.query("DELETE FROM project WHERE id = $1", [projectId]);
      if (squadId) await client.query("DELETE FROM squad WHERE id = $1", [squadId]);
      if (agentId) await client.query("DELETE FROM agent WHERE id = $1", [agentId]);
      if (runtimeId) await client.query("DELETE FROM agent_runtime WHERE id = $1", [runtimeId]);
    } finally {
      await client.end();
    }
  });

  test("runs agent, squad, human takeover, re-entry, and replay cases", async ({ page }, testInfo) => {
    const suffix = Date.now();
    const agentName = `Lifecycle Agent ${suffix}`;
    const squadName = `Lifecycle Squad ${suffix}`;
    const projectTitle = `Lifecycle E2E ${suffix}`;
    const issueTitle = `Lifecycle vertical slice ${suffix}`;

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    let userId = "";
    try {
      const user = await client.query<{ id: string }>(`SELECT id FROM "user" WHERE email = $1`, [api.getEmail()]);
      userId = user.rows[0]?.id ?? "";
      if (!userId) throw new Error("E2E user was not found");
      runtimeId = (await client.query<{ id: string }>(`
        INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
        VALUES ($1, $2, 'cloud', 'codex', 'online', '', '{}'::jsonb, $3)
        RETURNING id
      `, [workspaceId, `Lifecycle Runtime ${suffix}`, userId])).rows[0]?.id ?? "";
      agentId = (await client.query<{ id: string }>(`
        INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
        VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
        RETURNING id
      `, [workspaceId, agentName, runtimeId, userId])).rows[0]?.id ?? "";
      squadId = (await client.query<{ id: string }>(`
        INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
        VALUES ($1, $2, '', $3, $4)
        RETURNING id
      `, [workspaceId, squadName, agentId, userId])).rows[0]?.id ?? "";
    } finally {
      await client.end();
    }

    const project = await request<{ id: string }>(api, workspaceId, "/api/projects", {
      method: "POST",
      body: JSON.stringify({ title: projectTitle, status: "in_progress", priority: "high" }),
    });
    projectId = project.id;
    let lifecycle = await request<LifecycleResponse>(api, workspaceId, `/api/projects/${projectId}/issue-lifecycle`, {
      method: "PUT",
      body: JSON.stringify({ mode: "custom" }),
    });
    lifecycle = await updateStatus(api, workspaceId, lifecycle, "todo", {
      name: "Ready for Agent",
      description: "An agent starts automatically on entry.",
      color: "#2563eb",
      phase: "unstarted",
      entry_policy: {
        assignee: { type: "agent", id: agentId },
        executor: { type: "agent", id: agentId },
        instructions: "Implement the scoped change and report the result.",
        advance: "executor_may_transition",
      },
    });
    lifecycle = await updateStatus(api, workspaceId, lifecycle, "in_progress", {
      name: "Squad Build",
      description: "The squad leader coordinates this step.",
      color: "#7c3aed",
      phase: "started",
      entry_policy: {
        assignee: { type: "squad", id: squadId },
        executor: { type: "squad", id: squadId },
        instructions: "Coordinate the implementation as a squad.",
        advance: "human_confirms",
      },
    });
    lifecycle = await updateStatus(api, workspaceId, lifecycle, "in_review", {
      name: "Human Review",
      description: "A human reviews the result without an automatic run.",
      color: "#d97706",
      phase: "started",
      entry_policy: {
        assignee: { type: "human", id: userId },
        executor: { type: "none" },
        instructions: "",
        advance: "human_confirms",
      },
    });

    const todo = lifecycle.statuses.find((status) => status.legacy_status_key === "todo");
    if (!todo) throw new Error("Customized Todo status is missing");

    await page.goto(`/${(await api.getWorkspaces())[0]?.slug}/projects/${projectId}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(projectTitle).first()).toBeVisible();
    await expect(page.getByText("Customized for this project")).toBeVisible();
    await expect(page.getByText("Ready for Agent")).toBeVisible();
    await expect(page.getByText("Squad Build")).toBeVisible();
    await expect(page.getByText("Human Review")).toBeVisible();
    await capture(page, testInfo, "01-project-custom-lifecycle.png");

    const issue = await api.createIssue(issueTitle, { project_id: projectId, status: "todo", priority: "high" }) as IssueResponse;
    expect(issue.lifecycle_id).toBe(lifecycle.lifecycle.id);
    expect(issue.lifecycle_status_id).toBe(todo.id);
    expect(issue.assignee_type).toBe("agent");
    expect(issue.assignee_id).toBe(agentId);

    await page.goto(`/${(await api.getWorkspaces())[0]?.slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(issueTitle).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Ready for Agent", exact: true }).first()).toBeVisible();
    const automationSection = page.getByText("Automation", { exact: true }).locator("..").locator("..");
    await expect(automationSection).toBeVisible();
    await expect(automationSection.getByText(agentName)).toBeVisible();
    await expect(page.getByRole("button", { name: "Take over" })).toBeVisible();
    await capture(page, testInfo, "02-agent-entry-queued.png");

    await page.getByRole("button", { name: "Take over" }).click();
    await expect(page.getByRole("button", { name: "Take over" })).toBeHidden();
    let executions = await request<AutomationExecution[]>(api, workspaceId, `/api/issues/${issue.id}/automation-executions`);
    expect(executions).toHaveLength(1);
    expect(executions[0]).toMatchObject({ status: "superseded", executor_type: "agent", executor_id: agentId });

    await chooseStatus(page, "Ready for Agent", "Squad Build");
    await expect(automationSection.getByText(squadName)).toBeVisible();
    await expect(page.getByRole("button", { name: "Take over" })).toBeVisible();
    await capture(page, testInfo, "03-squad-entry-queued.png");

    await chooseStatus(page, "Squad Build", "Human Review");
    await expect(automationSection.getByText("Manual")).toBeVisible();
    await expect(automationSection.getByText("dormant")).toBeVisible();
    await expect(page.getByRole("button", { name: "Take over" })).toBeHidden();

    await chooseStatus(page, "Human Review", "Ready for Agent");
    await expect(automationSection.getByText(agentName)).toBeVisible();
    await expect(page.getByRole("button", { name: "Take over" })).toBeVisible();
    await expect(page.getByText(/changed status from Human Review to Ready for Agent/i)).toBeVisible();
    await capture(page, testInfo, "04-agent-reentry-queued.png");

    const current = await request<IssueResponse>(api, workspaceId, `/api/issues/${issue.id}`);
    const replay = await request<{ transition: unknown; execution: unknown; task_id: unknown }>(api, workspaceId, `/api/issues/${issue.id}/transitions`, {
      method: "POST",
      body: JSON.stringify({
        lifecycle_status_id: todo.id,
        expected_revision: current.revision,
        expected_transition_id: current.transition_id,
      }),
    });
    expect(replay).toMatchObject({ transition: null, execution: null, task_id: null });

    executions = await request<AutomationExecution[]>(api, workspaceId, `/api/issues/${issue.id}/automation-executions`);
    expect(executions).toHaveLength(4);
    expect(executions.map((execution) => execution.status)).toEqual([
      "queued",
      "dormant",
      "superseded",
      "superseded",
    ]);
    expect(executions[0]?.policy_snapshot.instructions).toBe("Implement the scoped change and report the result.");

    const verification = new pg.Client(DATABASE_URL);
    await verification.connect();
    try {
      const tasks = await verification.query<{
        status: string;
        is_leader_task: boolean;
        squad_id: string | null;
        automation_execution_id: string;
      }>(`
        SELECT status, is_leader_task, squad_id, automation_execution_id
        FROM agent_task_queue
        WHERE issue_id = $1
        ORDER BY created_at, id
      `, [issue.id]);
      expect(tasks.rows.map((task) => task.status)).toEqual(["cancelled", "cancelled", "queued"]);
      expect(tasks.rows[1]).toMatchObject({ is_leader_task: true, squad_id: squadId });
      expect(new Set(tasks.rows.map((task) => task.automation_execution_id)).size).toBe(3);
    } finally {
      await verification.end();
    }
  });
});
