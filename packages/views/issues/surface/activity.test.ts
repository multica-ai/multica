import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { deriveIssueSurfaceActivity } from "./activity";

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: overrides.id ?? "task-1",
    workspace_id: "ws-1",
    issue_id: undefined,
    agent_id: "agent-1",
    status: "queued",
    trigger_type: "manual",
    trigger_source: "manual",
    trigger_ref: null,
    prompt: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    started_at: null,
    completed_at: null,
    ...overrides,
  } as AgentTask;
}

describe("deriveIssueSurfaceActivity", () => {
  it("separates running work from queued-only issue activity", () => {
    const activity = deriveIssueSurfaceActivity([
      task({ id: "run-1", issue_id: "i-1", status: "running" }),
      task({ id: "queue-1", issue_id: "i-2", status: "queued" }),
      task({ id: "dispatch-1", issue_id: "i-2", status: "dispatched" }),
      task({ id: "wait-1", issue_id: "i-3", status: "waiting_local_directory" }),
      task({ id: "done-1", issue_id: "i-4", status: "completed" }),
      task({ id: "no-issue", issue_id: undefined, status: "running" }),
    ]);

    expect(activity.runningIssueIds).toEqual(new Set(["i-1"]));
    expect(activity.activityByIssueId.get("i-1")).toMatchObject({
      isWorking: true,
      isQueued: false,
    });
    expect(activity.activityByIssueId.get("i-2")).toMatchObject({
      isWorking: false,
      isQueued: true,
    });
    expect(activity.activityByIssueId.get("i-3")).toMatchObject({
      isWorking: false,
      isQueued: true,
    });
    expect(activity.activityByIssueId.has("i-4")).toBe(false);
  });
});
