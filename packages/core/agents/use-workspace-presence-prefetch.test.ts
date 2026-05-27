import { describe, expect, it } from "vitest";
import type { AgentTask } from "../types";
import { hasActiveAgentTasks } from "./use-workspace-presence-prefetch";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "rt-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-05-27T12:00:00Z",
    ...overrides,
  };
}

describe("hasActiveAgentTasks", () => {
  it("returns false for an empty or missing snapshot", () => {
    expect(hasActiveAgentTasks(undefined)).toBe(false);
    expect(hasActiveAgentTasks([])).toBe(false);
  });

  it("returns true while a snapshot contains queued, dispatched, running, or waiting tasks", () => {
    expect(hasActiveAgentTasks([makeTask({ status: "queued" })])).toBe(true);
    expect(hasActiveAgentTasks([makeTask({ status: "dispatched" })])).toBe(true);
    expect(hasActiveAgentTasks([makeTask({ status: "running" })])).toBe(true);
    expect(hasActiveAgentTasks([makeTask({ status: "waiting_local_directory" })])).toBe(true);
  });

  it("returns false once the authoritative snapshot only contains terminal tasks", () => {
    expect(
      hasActiveAgentTasks([
        makeTask({ id: "completed", status: "completed" }),
        makeTask({ id: "failed", status: "failed" }),
        makeTask({ id: "cancelled", status: "cancelled" }),
      ]),
    ).toBe(false);
  });

  it("keeps polling when active tasks coexist with the latest terminal rows", () => {
    expect(
      hasActiveAgentTasks([
        makeTask({ id: "completed", status: "completed" }),
        makeTask({ id: "running", status: "running" }),
      ]),
    ).toBe(true);
  });
});
