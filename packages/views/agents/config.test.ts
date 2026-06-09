import { describe, expect, it } from "vitest";
import { getTaskQueueDisplay } from "./config";

describe("getTaskQueueDisplay", () => {
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
