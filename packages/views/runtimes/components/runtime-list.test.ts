import { describe, expect, it } from "vitest";
import type { Agent, AgentTask } from "@multica/core/types";
import { buildWorkloadIndex } from "./runtime-list";

function agent(id: string, runtimeId: string, archivedAt: string | null = null): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: runtimeId,
    name: id,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: null,
    skills: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: archivedAt,
    archived_by: null,
  };
}

function task(agentId: string, status: AgentTask["status"]): AgentTask {
  return {
    id: `${agentId}-${status}`,
    agent_id: agentId,
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status,
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

describe("buildWorkloadIndex", () => {
  it("excludes archived agents and their tasks from runtime workload", () => {
    const index = buildWorkloadIndex(
      [
        agent("active-agent", "runtime-1"),
        agent("archived-agent", "runtime-1", "2026-01-02T00:00:00Z"),
      ],
      [
        task("active-agent", "running"),
        task("active-agent", "dispatched"),
        task("archived-agent", "running"),
      ],
    );

    expect(index.get("runtime-1")).toEqual({
      agentIds: ["active-agent"],
      runningCount: 1,
      queuedCount: 1,
    });
  });
});
