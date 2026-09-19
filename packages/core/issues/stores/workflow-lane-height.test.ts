// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import { mergeViewStatePersisted, viewStorePersistOptions, viewStoreSlice, type IssueViewState } from "./view-store";

describe("workflow lane heights", () => {
  it("persists independent heights and restores old snapshots with automatic sizing", () => {
    const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
    store.getState().setWorkflowLaneHeight("one", 650);
    store.getState().setWorkflowLaneHeight("two", 450);
    const snapshot = viewStorePersistOptions("test").partialize(store.getState());
    const defaults = viewStoreSlice(store.setState);
    expect(mergeViewStatePersisted(snapshot, defaults).workflowLaneHeights).toEqual({ one: 650, two: 450 });
    expect(mergeViewStatePersisted({}, defaults).workflowLaneHeights).toEqual({});
    store.getState().setWorkflowLaneHeight("one", null);
    expect(store.getState().workflowLaneHeights).toEqual({ two: 450 });
  });
  it("bounds stored heights and ignores invalid values", () => {
    const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
    store.getState().setWorkflowLaneHeight("one", 9999);
    store.getState().setWorkflowLaneHeight("two", -10);
    store.getState().setWorkflowLaneHeight("one", NaN);
    expect(store.getState().workflowLaneHeights).toEqual({ one: 1200, two: 240 });
    expect(mergeViewStatePersisted({ workflowLaneHeights: { tiny: -1, huge: 9999, text: "600", infinite: Infinity, valid: 501.5 } }, store.getState()).workflowLaneHeights)
      .toEqual({ tiny: 240, huge: 1200, valid: 502 });
    expect(mergeViewStatePersisted({ workflowLaneHeights: [] }, store.getState()).workflowLaneHeights).toEqual({ one: 1200, two: 240 });
  });
});
