import { beforeEach, describe, expect, it } from "vitest";
import { useRecentIssuesStore, selectRecentIssues } from "./recent-issues-store";

beforeEach(() => {
  useRecentIssuesStore.setState({ byWorkspace: {} });
});

describe("useRecentIssuesStore.recordVisit", () => {
  it("keeps visits namespaced by workspace id", () => {
    const { recordVisit } = useRecentIssuesStore.getState();
    recordVisit("ws-a", "issue-1");
    recordVisit("ws-b", "issue-2");

    const state = useRecentIssuesStore.getState().byWorkspace;
    expect(state["ws-a"]?.map((e) => e.id)).toEqual(["issue-1"]);
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["issue-2"]);
  });

  it("moves the most recent visit to the front and dedupes", () => {
    const { recordVisit } = useRecentIssuesStore.getState();
    recordVisit("ws-a", "issue-1");
    recordVisit("ws-a", "issue-2");
    recordVisit("ws-a", "issue-1");

    const ids = useRecentIssuesStore
      .getState()
      .byWorkspace["ws-a"]?.map((e) => e.id);
    expect(ids).toEqual(["issue-1", "issue-2"]);
  });

  it("caps each workspace's bucket at 20 entries", () => {
    const { recordVisit } = useRecentIssuesStore.getState();
    for (let i = 0; i < 25; i++) recordVisit("ws-a", `issue-${i}`);
    expect(useRecentIssuesStore.getState().byWorkspace["ws-a"]).toHaveLength(20);
  });
});

describe("useRecentIssuesStore.pruneWorkspaces", () => {
  it("drops buckets for workspaces not in the active set", () => {
    const { recordVisit, pruneWorkspaces } = useRecentIssuesStore.getState();
    recordVisit("ws-a", "issue-1");
    recordVisit("ws-b", "issue-2");
    recordVisit("ws-c", "issue-3");

    pruneWorkspaces(["ws-a", "ws-c"]);
    const state = useRecentIssuesStore.getState().byWorkspace;
    expect(Object.keys(state).sort()).toEqual(["ws-a", "ws-c"]);
  });

  it("is a no-op when every bucket is still active", () => {
    const { recordVisit, pruneWorkspaces } = useRecentIssuesStore.getState();
    recordVisit("ws-a", "issue-1");
    const before = useRecentIssuesStore.getState().byWorkspace;
    pruneWorkspaces(["ws-a"]);
    expect(useRecentIssuesStore.getState().byWorkspace).toBe(before);
  });
});

describe("selectRecentIssues", () => {
  it("returns the bucket for the given workspace", () => {
    useRecentIssuesStore.getState().recordVisit("ws-a", "issue-1");
    const items = selectRecentIssues("ws-a")(useRecentIssuesStore.getState());
    expect(items.map((e) => e.id)).toEqual(["issue-1"]);
  });

  it("returns a stable empty array when wsId is null or unknown", () => {
    const a = selectRecentIssues(null)(useRecentIssuesStore.getState());
    const b = selectRecentIssues(null)(useRecentIssuesStore.getState());
    const c = selectRecentIssues("missing")(useRecentIssuesStore.getState());
    expect(a).toBe(b);
    expect(a).toBe(c);
    expect(a).toEqual([]);
  });
});
