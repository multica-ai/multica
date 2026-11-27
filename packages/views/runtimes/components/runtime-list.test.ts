import { describe, expect, it } from "vitest";
import type { Agent, AgentRuntime, AgentTask } from "@multica/core/types";
import { buildWorkloadIndex, canReadRuntimeUsage } from "./runtime-list";

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
    custom_args: [],
    visibility: "private",
    permission_mode: "private",
    invocation_targets: [],
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

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Runtime",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "",
    status: "online",
    device_info: "Mac",
    metadata: {},
    owner_id: "user-2",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("canReadRuntimeUsage", () => {
  it("does not load usage for an admin viewing another member's private runtime", () => {
    expect(canReadRuntimeUsage(makeRuntime(), "admin-1")).toBe(false);
  });

  it("allows usage for a public teammate runtime", () => {
    expect(
      canReadRuntimeUsage(makeRuntime({ visibility: "public" }), "member-1"),
    ).toBe(true);
  });
});
