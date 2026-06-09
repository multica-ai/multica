import { describe, expect, it } from "vitest";
import { getTaskQueueBucket, getTaskQueueDisplay } from "./config";

describe("getTaskQueueDisplay", () => {
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
});
