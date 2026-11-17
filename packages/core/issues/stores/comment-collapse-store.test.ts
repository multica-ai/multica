// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { useCommentCollapseStore } from "./comment-collapse-store";

describe("comment collapse store", () => {
  beforeEach(() => {
    useCommentCollapseStore.setState({ collapsedByIssue: {}, expandedByIssue: {} });
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

    expandAll("issue-1", ["c1", "c2"]);

    const state = useCommentCollapseStore.getState();
    expect(state.collapsedByIssue["issue-1"]).toBeUndefined();
    expect(state.collapsedByIssue["issue-2"]).toEqual(["c9"]);

    const before = state.collapsedByIssue;
    expandAll("issue-1", ["c1", "c2"]);
    expect(useCommentCollapseStore.getState().collapsedByIssue).toBe(before);
  });
});


describe("comment thread starting defaults", () => {
  beforeEach(async () => {
    localStorage.clear();
    setCurrentWorkspace("thread-default-test", "ws-thread-default-test");
    await Promise.resolve();
    useCommentCollapseStore.setState({ collapsedByIssue: {}, expandedByIssue: {} });
  });

  afterEach(() => setCurrentWorkspace(null, null));

  it("starts untouched threads folded without treating that default as a manual choice", () => {
    const state = useCommentCollapseStore.getState();
    expect(state.isCollapsed("issue-1", "older", true)).toBe(true);
    expect(state.isCollapsed("issue-1", "older", false)).toBe(false);
  });

  it("manually opens a default-folded thread and keeps it open after the default changes", () => {
    useCommentCollapseStore.getState().toggle("issue-1", "older", true);
    const state = useCommentCollapseStore.getState();
    expect(state.isCollapsed("issue-1", "older", true)).toBe(false);
    expect(state.isCollapsed("issue-1", "older", false)).toBe(false);
    expect(state.isCollapsed("issue-1", "untouched", true)).toBe(true);
  });

  it("keeps a manual fold when the starting default changes", () => {
    useCommentCollapseStore.getState().toggle("issue-1", "older", false);
    const state = useCommentCollapseStore.getState();
    expect(state.isCollapsed("issue-1", "older", false)).toBe(true);
    expect(state.isCollapsed("issue-1", "older", true)).toBe(true);
  });

  it("unfold-all opens current roots under a folded default and persists that choice", async () => {
    const store = useCommentCollapseStore;
    store.getState().expandAll("issue-1", ["older", "latest", "older"]);
    const storage = store.persist.getOptions().storage!;
    const saved = await storage.getItem("multica_comment_collapse");
    store.setState({ collapsedByIssue: {}, expandedByIssue: {} });
    await storage.setItem("multica_comment_collapse", saved!);
    await store.persist.rehydrate();
    const state = store.getState();
    expect(state.isCollapsed("issue-1", "older", true)).toBe(false);
    expect(state.isCollapsed("issue-1", "latest", true)).toBe(false);
    expect(state.isCollapsed("issue-1", "new-root", true)).toBe(true);
  });

  it("retains previously stored manual collapses", async () => {
    const store = useCommentCollapseStore;
    await store.persist.getOptions().storage!.setItem("multica_comment_collapse", {
      state: { collapsedByIssue: { "issue-1": ["old-root"] } }, version: 0,
    });
    await store.persist.rehydrate();
    expect(store.getState().isCollapsed("issue-1", "old-root", false)).toBe(true);
  });
});
