import { describe, expect, it } from "vitest";

import { AgentTaskSchema } from "./schemas";

describe("AgentTaskSchema", () => {
  it("preserves refined failure reasons from newer backends", () => {
    const task = AgentTaskSchema.parse({
      id: "task-1",
      agent_id: "agent-1",
      runtime_id: "runtime-1",
      issue_id: "issue-1",
      status: "failed",
      priority: 0,
      created_at: "2026-08-24T00:00:00Z",
      failure_reason: "waiting_local_directory_abandoned",
    });

    expect(task.failure_reason).toBe("waiting_local_directory_abandoned");
  });

  it("normalizes empty and null failure reasons away", () => {
    expect(AgentTaskSchema.parse(baseTask({ failure_reason: "" })).failure_reason).toBeUndefined();
    expect(AgentTaskSchema.parse(baseTask({ failure_reason: null })).failure_reason).toBeUndefined();
  });
});

function baseTask(overrides: Record<string, unknown>) {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    created_at: "2026-08-24T00:00:00Z",
    ...overrides,
  };
}
