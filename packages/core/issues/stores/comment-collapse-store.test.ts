// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useCommentCollapseStore } from "./comment-collapse-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

describe("comment collapse store", () => {
  beforeEach(() => {
    useCommentCollapseStore.setState({ collapsedByIssue: {} });
  });

  it("toggle collapses and expands a single comment", () => {
    const { toggle } = useCommentCollapseStore.getState();

    toggle("issue-1", "c1");
    expect(useCommentCollapseStore.getState().isCollapsed("issue-1", "c1")).toBe(true);
    expect(useCommentCollapseStore.getState().isCollapsed("issue-1", "c2")).toBe(false);

    toggle("issue-1", "c1");
    expect(useCommentCollapseStore.getState().isCollapsed("issue-1", "c1")).toBe(false);
    expect(useCommentCollapseStore.getState().collapsedByIssue).toEqual({});
  });

  it("collapseAll replaces the issue's collapsed set and dedupes ids", () => {
    const { toggle, collapseAll } = useCommentCollapseStore.getState();
    toggle("issue-1", "stale");
    toggle("issue-2", "kept");

    collapseAll("issue-1", ["c1", "c2", "c1"]);

    const state = useCommentCollapseStore.getState();
    expect(state.collapsedByIssue["issue-1"]).toEqual(["c1", "c2"]);
    expect(state.isCollapsed("issue-1", "stale")).toBe(false);
    expect(state.isCollapsed("issue-2", "kept")).toBe(true);
  });

  it("collapseAll with no ids clears the issue entry", () => {
    const { collapseAll } = useCommentCollapseStore.getState();
    collapseAll("issue-1", ["c1"]);

    collapseAll("issue-1", []);
    expect(useCommentCollapseStore.getState().collapsedByIssue).toEqual({});

    const before = useCommentCollapseStore.getState().collapsedByIssue;
    collapseAll("issue-1", []);
    expect(useCommentCollapseStore.getState().collapsedByIssue).toBe(before);
  });

  it("expandAll clears one issue without touching others", () => {
    const { collapseAll, expandAll } = useCommentCollapseStore.getState();
    collapseAll("issue-1", ["c1", "c2"]);
    collapseAll("issue-2", ["c9"]);

    expandAll("issue-1");

    const state = useCommentCollapseStore.getState();
    expect(state.collapsedByIssue["issue-1"]).toBeUndefined();
    expect(state.collapsedByIssue["issue-2"]).toEqual(["c9"]);

    const before = state.collapsedByIssue;
    expandAll("issue-1");
    expect(useCommentCollapseStore.getState().collapsedByIssue).toBe(before);
  });
});
