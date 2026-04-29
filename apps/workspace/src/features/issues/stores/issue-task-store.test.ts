import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/shared/api";
import type { AgentTask } from "@/shared/types";
import { useIssueTaskStore } from "./issue-task-store";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "dispatched",
    priority: 1,
    dispatched_at: "2026-04-11T08:00:00Z",
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-04-11T08:00:00Z",
    ...overrides,
  };
}

describe("useIssueTaskStore", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useIssueTaskStore.setState({
      byIssueId: {},
      taskIssueIndex: {},
      observedIssueRefs: {},
    });
  });

  it("loads active tasks and applies progress updates to tracked issues", async () => {
    vi.spyOn(api, "getActiveTaskForIssue").mockResolvedValue({
      task: makeTask(),
    });

    await useIssueTaskStore.getState().refreshIssue("issue-1");

    let state = useIssueTaskStore.getState();
    expect(state.byIssueId["issue-1"]?.loaded).toBe(true);
    expect(state.byIssueId["issue-1"]?.task?.id).toBe("task-1");
    expect(state.taskIssueIndex["task-1"]).toBe("issue-1");

    state.setProgress("task-1", "Writing patch", 2, 4);

    state = useIssueTaskStore.getState();
    expect(state.byIssueId["issue-1"]?.summary).toBe("Writing patch");
    expect(state.byIssueId["issue-1"]?.step).toBe(2);
    expect(state.byIssueId["issue-1"]?.total).toBe(4);
    expect(state.byIssueId["issue-1"]?.task?.status).toBe("running");

    state.clearIssueTask("issue-1");

    state = useIssueTaskStore.getState();
    expect(state.byIssueId["issue-1"]?.task).toBeNull();
    expect(state.taskIssueIndex["task-1"]).toBeUndefined();
  });

  it("refreshes only observed issues for reconnect-driven sync", async () => {
    const getActiveTaskForIssue = vi.spyOn(api, "getActiveTaskForIssue").mockResolvedValue({
      task: null,
    });

    const store = useIssueTaskStore.getState();
    store.registerIssue("issue-1");
    store.registerIssue("issue-2");

    await store.refreshObservedIssues();

    expect(getActiveTaskForIssue).toHaveBeenCalledTimes(2);
    expect(getActiveTaskForIssue).toHaveBeenCalledWith("issue-1");
    expect(getActiveTaskForIssue).toHaveBeenCalledWith("issue-2");
  });
});