// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import { commentSupplementReceipts } from "@multica/core/issues/run-steering";
import type { AgentTask, CommentSupplementReceipt, TimelineEntry } from "@multica/core/types";
import {
  buildDeliveredSteersByTask,
  buildWakeupIdByTask,
  IssueRunLookupProvider,
  useDeliveredSteersForTask,
  useTaskWakeupId,
} from "./issue-run-lookup-context";

vi.mock("@multica/core/issues/run-steering", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues/run-steering")>();
  return { ...actual, commentSupplementReceipts: vi.fn(actual.commentSupplementReceipts) };
});

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1", agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "completed", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
    completed_at: "2026-09-07T00:01:00Z", result: null, error: null,
    ...overrides,
  };
}

function comment(id: string, supplements: TimelineEntry["supplements"]): TimelineEntry {
  return {
    id, type: "comment", actor_type: "member", actor_id: "user", content: `Content ${id}`,
    created_at: "2026-09-07T00:00:00Z", supplements,
  } as TimelineEntry;
}

const delivered = (taskId: string, deliveredAt: string): CommentSupplementReceipt => ({
  task_id: taskId, agent_id: "agent", status: "delivered", delivered_at: deliveredAt,
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("issue run lookup builders", () => {
  it("groups each comment's first delivered receipt by task in one pass", () => {
    const entries: TimelineEntry[] = [
      comment("one", [
        { task_id: "task-1", agent_id: "agent", status: "pending" },
        delivered("task-1", "2026-09-07T00:00:02Z"),
        delivered("task-1", "2026-09-07T00:00:03Z"),
      ]),
      { id: "activity", type: "activity", actor_type: "system", actor_id: "", action: "updated", details: {}, created_at: "2026-09-07T00:00:01Z" } as TimelineEntry,
      comment("two", [delivered("task-2", "2026-09-07T00:00:04Z")]),
      comment("three", [delivered("task-1", "2026-09-07T00:00:05Z")]),
    ];

    const result = buildDeliveredSteersByTask(entries);
    expect([...result.keys()]).toEqual(["task-1", "task-2"]);
    expect(result.get("task-1")?.map((steer) => [steer.id, steer.deliveredAt])).toEqual([
      ["one", "2026-09-07T00:00:02Z"],
      ["three", "2026-09-07T00:00:05Z"],
    ]);
    expect(result.get("task-2")?.[0]).toMatchObject({
      id: "two", actorType: "member", actorId: "user", content: "Content two",
    });
    expect(commentSupplementReceipts).toHaveBeenCalledTimes(3);
  });

  it("indexes every task's wakeup id, including an absent id", () => {
    const result = buildWakeupIdByTask([
      task({ id: "task-1", wakeup_id: "wakeup-1" }),
      task({ id: "task-2", wakeup_id: undefined }),
    ]);
    expect([...result]).toEqual([["task-1", "wakeup-1"], ["task-2", undefined]]);
  });
});

describe("issue run lookup hooks", () => {
  it("shares provider-built maps across consumers without rescanning cache data", () => {
    const timeline = [comment("one", [delivered("task-1", "2026-09-07T00:00:02Z")])];
    const tasks = [task({ wakeup_id: "wakeup-1" })];
    const client = new QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        <IssueRunLookupProvider timeline={timeline} tasks={tasks}>{children}</IssueRunLookupProvider>
      </QueryClientProvider>
    );

    const { result, rerender } = renderHook(() => ({
      first: useDeliveredSteersForTask("task-1"),
      missing: useDeliveredSteersForTask("task-2"),
      wakeupId: useTaskWakeupId("issue", "task-1"),
    }), { wrapper });

    expect(result.current.first.map((steer) => steer.id)).toEqual(["one"]);
    expect(result.current.missing).toEqual([]);
    expect(result.current.wakeupId).toBe("wakeup-1");
    expect(commentSupplementReceipts).toHaveBeenCalledTimes(1);
    rerender();
    expect(commentSupplementReceipts).toHaveBeenCalledTimes(1);
    expect(client.getQueryCache().find({ queryKey: issueKeys.tasks("issue") })).toBeUndefined();
    expect(client.getQueryCache().find({ queryKey: issueKeys.timeline("issue") })).toBeUndefined();
    expect(client.getQueryCache().find({ queryKey: issueKeys.timeline("") })).toBeUndefined();
  });

  it("keeps the original query-cache fallback for standalone rendering", () => {
    const timeline = [comment("one", [delivered("task-1", "2026-09-07T00:00:02Z")])];
    const tasks = [task({ wakeup_id: "wakeup-1" })];
    const client = new QueryClient();
    client.setQueryData(issueKeys.timeline("issue"), timeline);
    client.setQueryData(issueKeys.tasks("issue"), tasks);
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;

    const { result } = renderHook(() => ({
      steers: useDeliveredSteersForTask("task-1", "issue"),
      wakeupId: useTaskWakeupId("issue", "task-1"),
    }), { wrapper });

    expect(result.current.steers.map((steer) => steer.id)).toEqual(["one"]);
    expect(result.current.wakeupId).toBe("wakeup-1");
  });
});
