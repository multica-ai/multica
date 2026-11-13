// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import {
  mergeViewStatePersisted,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";

describe("board sub-issue nesting", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  });

  it("nests sub-issues with every parent expanded by default", () => {
    expect(store.getState().boardHierarchy).toBe(true);
    expect(store.getState().boardCollapsedParents).toEqual([]);
  });

  it("toggles nesting and per-parent collapse independently of the table", () => {
    store.getState().toggleBoardHierarchy();
    store.getState().toggleBoardParentCollapsed("p1");
    expect(store.getState().boardHierarchy).toBe(false);
    expect(store.getState().boardCollapsedParents).toEqual(["p1"]);
    expect(store.getState().tableHierarchy).toBe(true);
    expect(store.getState().tableCollapsedParents).toEqual([]);

    store.getState().toggleBoardParentCollapsed("p1");
    expect(store.getState().boardCollapsedParents).toEqual([]);
  });

  it("falls back to defaults for a malformed persisted collapse list", () => {
    const defaults = store.getState();
    expect(
      mergeViewStatePersisted({ boardCollapsedParents: "p1" }, defaults)
        .boardCollapsedParents,
    ).toEqual([]);
    expect(
      mergeViewStatePersisted({ boardCollapsedParents: ["p1"] }, defaults)
        .boardCollapsedParents,
    ).toEqual(["p1"]);
  });
});
