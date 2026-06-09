import { describe, expect, it } from "vitest";
import { LONG_RUNNING_TASK_MS, getTaskQueueBucket, getTaskQueueDisplay, getTaskReviewFlag } from "./config";

describe("agent task helpers", () => {
  it("classifies non-blocked queued tasks separately from running work", () => {
    expect(getTaskQueueBucket({ status: "queued" } as any, { blocked_by_count: 0 } as any)).toBe("queued");
    expect(getTaskQueueBucket({ status: "dispatched" } as any, { blocked_by_count: 0 } as any)).toBe("running");
    expect(getTaskQueueBucket({ status: "failed" } as any, { blocked_by_count: 0 } as any)).toBe("failed");
  });

  it("marks queued tasks with unresolved blockers as blocked", () => {
    const display = getTaskQueueDisplay({ status: "queued" } as any, { blocked_by_count: 2 } as any);

    expect(display.label).toBe("Blocked");
    expect(display.tone).toBe("blocked");
    expect(display.detail).toBe("Waiting on 2 unresolved dependencies");
  });

  it("keeps normal queued tasks in the default queue state", () => {
    const display = getTaskQueueDisplay({ status: "queued" } as any, { blocked_by_count: 0 } as any);

    expect(display.label).toBe("Queued");
    expect(display.tone).toBe("default");
    expect(display.detail).toBeNull();
  });

  it("flags failed tasks for manual review", () => {
    const review = getTaskReviewFlag({ status: "failed", error: "Runtime disconnected during tool execution", started_at: null, dispatched_at: null } as any);

    expect(review).toEqual({
      tone: "failed",
      label: "Failed",
      detail: "Runtime disconnected during tool execution",
    });
  });

  it("flags long-running active tasks after the review threshold", () => {
    const now = Date.parse("2026-06-09T12:00:00.000Z");
    const review = getTaskReviewFlag(
      {
        status: "running",
        error: null,
        started_at: new Date(now - LONG_RUNNING_TASK_MS - 5 * 60_000).toISOString(),
        dispatched_at: null,
      } as any,
      now,
    );

    expect(review?.tone).toBe("long-running");
    expect(review?.detail).toContain("Active for 35m");
  });
});
