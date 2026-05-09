import { describe, expect, it } from "vitest";
import type { Agent, AgentTask } from "@multica/core/types";
import { buildWorkloadIndex } from "./runtime-list";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "private",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "gpt-5.4",
    owner_id: "user-1",
    skills: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
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
    runtime_id: "runtime-1",
    attempt: 1,
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
