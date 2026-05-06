import { describe, expect, it } from "vitest";
import type { Agent, AgentTask } from "@multica/core/types";
import { buildWorkloadIndex } from "./runtime-list";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    name: "Agent",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    visibility: "private",
    status: "idle",
    max_concurrent_tasks: 1,
    owner_id: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    description: "",
    runtime_id: "runtime-1",
    instructions: "",
    archived_at: null,
    archived_by: null,
    custom_env: {},
    custom_args: [],
    mcp_config: null,
    model: "gpt-5.4",
    skills: [],
    ...overrides,
  };
}

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    issue_id: "issue-1",
    status: "running",
    priority: 1,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    context: null,
    runtime_id: "runtime-1",
    session_id: null,
    work_dir: null,
    trigger_comment_id: null,
    chat_session_id: null,
    autopilot_run_id: null,
    attempt: 1,
    max_attempts: 1,
    parent_task_id: null,
    failure_reason: null,
    last_heartbeat_at: null,
    trigger_summary: null,
    force_fresh_session: false,
    ...overrides,
  };
}

describe("buildWorkloadIndex", () => {
  it("excludes archived agents from runtime agent counts and workload", () => {
    const activeAgent = makeAgent({ id: "active-agent" });
    const archivedAgent = makeAgent({
      id: "archived-agent",
      archived_at: "2026-01-02T00:00:00Z",
    });

    const tasks = [
      makeTask({ id: "active-task", agent_id: activeAgent.id, status: "running" }),
      makeTask({ id: "archived-task", agent_id: archivedAgent.id, status: "queued" }),
    ];

    const workload = buildWorkloadIndex([activeAgent, archivedAgent], tasks).get("runtime-1");

    expect(workload).toEqual({
      agentIds: [activeAgent.id],
      runningCount: 1,
      queuedCount: 0,
    });
  });
});
