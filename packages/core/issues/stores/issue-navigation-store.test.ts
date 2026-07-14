import { beforeEach, describe, expect, it } from "vitest";
import {
  useIssueNavigationStore,
  getIssueSiblings,
} from "./issue-navigation-store";

const COLUMNS = {
  "status:todo": ["a", "b", "c"],
  "status:in_progress": ["d", "e"],
  "status:done": [],
};

describe("getIssueSiblings", () => {
  it("returns both neighbours for an issue in the middle of a column", () => {
    expect(getIssueSiblings(COLUMNS, "b")).toEqual({
      hasContext: true,
      prevId: "a",
      nextId: "c",
    });
  });

  it("has no previous at the top of a column", () => {
    expect(getIssueSiblings(COLUMNS, "a")).toEqual({
      hasContext: true,
      prevId: null,
      nextId: "b",
    });
  });

  it("has no next at the bottom of a column", () => {
    expect(getIssueSiblings(COLUMNS, "c")).toEqual({
      hasContext: true,
      prevId: "b",
      nextId: null,
    });
  });

  it("has context but no neighbours for a single-issue column", () => {
    // The disabled-at-both-ends case the toolbar relies on: the buttons render
    // (hasContext) but both are greyed out.
    expect(getIssueSiblings({ "status:todo": ["only"] }, "only")).toEqual({
      hasContext: true,
      prevId: null,
      nextId: null,
    });
  });

  it("navigates within the issue's own column, never across columns", () => {
    // "d" is the first card of the in_progress column — its previous is null,
    // not "c" (the last todo card).
    expect(getIssueSiblings(COLUMNS, "d")).toEqual({
      hasContext: true,
      prevId: null,
      nextId: "e",
    });
  });

  it("reports no context when the issue isn't in any published column", () => {
    expect(getIssueSiblings(COLUMNS, "missing")).toEqual({
      hasContext: false,
      prevId: null,
      nextId: null,
    });
  });

  it("reports no context against an empty snapshot (deep link, never visited a list)", () => {
    expect(getIssueSiblings({}, "a")).toEqual({
      hasContext: false,
      prevId: null,
      nextId: null,
    });
  });

  it("reports no context when nothing was published for the workspace", () => {
    expect(getIssueSiblings(undefined, "a")).toEqual({
      hasContext: false,
      prevId: null,
      nextId: null,
    });
  });
});

describe("useIssueNavigationStore", () => {
  beforeEach(() => {
    useIssueNavigationStore.setState({ byWorkspace: {} });
  });

  it("publishes and clears columns per workspace", () => {
    useIssueNavigationStore.getState().setColumns("ws-1", COLUMNS);
    expect(useIssueNavigationStore.getState().byWorkspace["ws-1"]).toBe(COLUMNS);

    useIssueNavigationStore.getState().clear("ws-1");
    expect(useIssueNavigationStore.getState().byWorkspace["ws-1"]).toBeUndefined();
  });

  it("keeps each workspace's columns independent", () => {
    const a = { "status:todo": ["a"] };
    const b = { "status:todo": ["b"] };
    useIssueNavigationStore.getState().setColumns("ws-a", a);
    useIssueNavigationStore.getState().setColumns("ws-b", b);

    // Clearing one workspace (e.g. its swimlane mounting) leaves the other's
    // open-detail context intact — the desktop multi-tab case.
    useIssueNavigationStore.getState().clear("ws-b");
    expect(useIssueNavigationStore.getState().byWorkspace["ws-a"]).toBe(a);
    expect(useIssueNavigationStore.getState().byWorkspace["ws-b"]).toBeUndefined();
  });

  it("keeps the state reference stable when clearing an absent workspace", () => {
    const before = useIssueNavigationStore.getState().byWorkspace;
    useIssueNavigationStore.getState().clear("ws-never-set");
    expect(useIssueNavigationStore.getState().byWorkspace).toBe(before);
  });
});
