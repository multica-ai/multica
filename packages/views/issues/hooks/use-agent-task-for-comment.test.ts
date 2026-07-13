import { createElement, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types/agent";
import { useAgentTaskForComment } from "./use-agent-task-for-comment";

const issueId = "00000000-0000-0000-0000-0000000000aa";
const taskId = "00000000-0000-0000-0000-0000000000bb";
const otherTaskId = "00000000-0000-0000-0000-0000000000cc";

// Stub AgentTask: the hook doesn't introspect the payload beyond id +
// issue_id (the workspace guard compares t.issue_id === issueId), so a
// minimal cast suffices. Pinning the real type would require fixtures
// the package doesn't need; this assertion is the lightest expression
// of intent.
const sampleTask = {
  id: taskId,
  issue_id: issueId,
  agent_id: "00000000-0000-0000-0000-000000000001",
  status: "completed",
  kind: "comment",
} as unknown as AgentTask;

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client: queryClient }, children);
  };
}

describe("useAgentTaskForComment", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    queryClient.clear();
  });

  it("returns null when sourceTaskId is null", () => {
    const { result } = renderHook(() => useAgentTaskForComment(issueId, null), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toBeNull();
  });

  it("returns null when the issue cache is not populated", () => {
    const { result } = renderHook(() => useAgentTaskForComment(issueId, taskId), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toBeNull();
  });

  it("returns the matching AgentTask when the cache contains it", () => {
    queryClient.setQueryData<AgentTask[]>(issueKeys.tasks(issueId), [sampleTask]);
    const { result } = renderHook(() => useAgentTaskForComment(issueId, taskId), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toEqual(sampleTask);
  });

  it("returns null when sourceTaskId resolves to a task in a different issue", () => {
    // Defensive: even if the cache contains the sourceTaskId, the resolver
    // rejects when t.issue_id !== issueId so cross-issue leakage is impossible
    // (e.g. when a corrupted source_task_id references a task from another
    // workspace). The GC explainer fires instead of opening the wrong run.
    const otherIssueId = "00000000-0000-0000-0000-0000000000ff";
    queryClient.setQueryData<AgentTask[]>(issueKeys.tasks(otherIssueId), [sampleTask]);
    const { result } = renderHook(() => useAgentTaskForComment(issueId, taskId), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toBeNull();
  });

  it("returns null when sourceTaskId is non-null but no task matches", () => {
    queryClient.setQueryData<AgentTask[]>(issueKeys.tasks(issueId), [sampleTask]);
    const { result } = renderHook(() => useAgentTaskForComment(issueId, otherTaskId), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toBeNull();
  });

  it("returns a stable reference across re-renders when the cache is unchanged", () => {
    queryClient.setQueryData<AgentTask[]>(issueKeys.tasks(issueId), [sampleTask]);
    const { result, rerender } = renderHook(() => useAgentTaskForComment(issueId, taskId), {
      wrapper: createWrapper(queryClient),
    });
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});