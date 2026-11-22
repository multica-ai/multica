import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useInboxSelectionStore } from "./selection-store";

describe("useInboxSelectionStore transitions", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-23T00:00:00Z"));
    useInboxSelectionStore.setState({ transitionByWorkspace: {} });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("consumes a matching directional transition once", () => {
    const store = useInboxSelectionStore.getState();
    store.recordTransition("ws-1", "issue-2", "next");

    expect(store.consumeTransition("ws-1", "issue-other")).toBeNull();
    expect(store.consumeTransition("ws-1", "issue-2")).toBe("next");
    expect(store.consumeTransition("ws-1", "issue-2")).toBeNull();
  });

  it("supports prev direction for undo return", () => {
    const store = useInboxSelectionStore.getState();
    store.recordTransition("ws-1", "issue-1", "prev");
    expect(store.consumeTransition("ws-1", "issue-1")).toBe("prev");
  });

  it("expires stale transitions after 3 seconds", () => {
    const store = useInboxSelectionStore.getState();
    store.recordTransition("ws-1", "issue-2", "next");
    vi.advanceTimersByTime(3_001);

    expect(store.consumeTransition("ws-1", "issue-2")).toBeNull();
    expect(useInboxSelectionStore.getState().transitionByWorkspace["ws-1"]).toBeUndefined();
  });
});
