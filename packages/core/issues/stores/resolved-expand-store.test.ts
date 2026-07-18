import { beforeEach, describe, expect, it } from "vitest";
import { selectExpandedResolved, useResolvedExpandStore } from "./resolved-expand-store";

describe("resolved expand store", () => {
  beforeEach(() => {
    useResolvedExpandStore.setState({ expandedByIssue: {} });
  });

  it("setExpanded adds and removes ids per issue", () => {
    const { setExpanded } = useResolvedExpandStore.getState();

    setExpanded("issue-1", "c1", true);
    setExpanded("issue-1", "c2", true);
    setExpanded("issue-2", "c9", true);

    const state = useResolvedExpandStore.getState();
    expect([...state.expandedByIssue["issue-1"]!]).toEqual(["c1", "c2"]);
    expect([...state.expandedByIssue["issue-2"]!]).toEqual(["c9"]);

    setExpanded("issue-1", "c1", false);
    expect([...useResolvedExpandStore.getState().expandedByIssue["issue-1"]!]).toEqual(["c2"]);
  });

  it("setExpanded drops the issue key when its last id is removed", () => {
    const { setExpanded } = useResolvedExpandStore.getState();
    setExpanded("issue-1", "c1", true);
    setExpanded("issue-1", "c1", false);

    expect(useResolvedExpandStore.getState().expandedByIssue).toEqual({});
  });

  it("setExpanded is a no-op when the id is already in the requested state", () => {
    const { setExpanded } = useResolvedExpandStore.getState();
    setExpanded("issue-1", "c1", true);
    const before = useResolvedExpandStore.getState().expandedByIssue;

    setExpanded("issue-1", "c1", true);
    setExpanded("issue-1", "missing", false);

    expect(useResolvedExpandStore.getState().expandedByIssue).toBe(before);
  });

  it("expandAll unions ids into the issue's set and ignores empty input", () => {
    const { setExpanded, expandAll } = useResolvedExpandStore.getState();
    setExpanded("issue-1", "c1", true);

    expandAll("issue-1", ["c2", "c3", "c2"]);
    expect([...useResolvedExpandStore.getState().expandedByIssue["issue-1"]!].sort()).toEqual([
      "c1",
      "c2",
      "c3",
    ]);

    const before = useResolvedExpandStore.getState().expandedByIssue;
    expandAll("issue-1", []);
    expect(useResolvedExpandStore.getState().expandedByIssue).toBe(before);
  });

  it("collapseAll clears one issue without touching others", () => {
    const { expandAll, collapseAll } = useResolvedExpandStore.getState();
    expandAll("issue-1", ["c1", "c2"]);
    expandAll("issue-2", ["c9"]);

    collapseAll("issue-1");

    const state = useResolvedExpandStore.getState();
    expect(state.expandedByIssue["issue-1"]).toBeUndefined();
    expect([...state.expandedByIssue["issue-2"]!]).toEqual(["c9"]);

    const before = state.expandedByIssue;
    collapseAll("issue-1");
    expect(useResolvedExpandStore.getState().expandedByIssue).toBe(before);
  });

  it("selectExpandedResolved returns a stable empty set for untracked issues", () => {
    const select = selectExpandedResolved("issue-1");
    const a = select(useResolvedExpandStore.getState());
    useResolvedExpandStore.getState().setExpanded("issue-2", "c9", true);
    const b = select(useResolvedExpandStore.getState());

    expect(a.size).toBe(0);
    expect(b).toBe(a);
  });
});
