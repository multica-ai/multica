import { beforeEach, describe, expect, it } from "vitest";
import type { Issue, Project } from "../types";
import { selectCommandBarContexts, useCommandBarContextStore } from "./context-store";
import { selectRecentVisits, useRecentVisitsStore } from "./recent-store";

const issue = { id: "issue-1", identifier: "MUL-1" } as Issue;
const project = { id: "project-1", title: "Launch" } as Project;

describe("useCommandBarContextStore", () => {
  beforeEach(() => {
    useCommandBarContextStore.setState({ entries: [] });
  });

  it("lists the newest registration first", () => {
    const { register } = useCommandBarContextStore.getState();
    register("project-page", { kind: "project", project });
    register("issue-page", { kind: "issue", issue });

    const kinds = selectCommandBarContexts(useCommandBarContextStore.getState()).map(
      (entry) => entry.context.kind,
    );
    expect(kinds).toEqual(["issue", "project"]);
  });

  it("updates a registration in place and ignores an identical one", () => {
    const { register } = useCommandBarContextStore.getState();
    const first = { kind: "issue", issue } as const;
    register("issue-page", first);
    register("project-page", { kind: "project", project });
    const before = useCommandBarContextStore.getState().entries;

    register("issue-page", first);
    expect(useCommandBarContextStore.getState().entries).toBe(before);

    const renamed = { kind: "issue", issue: { ...issue, title: "Renamed" } } as const;
    register("issue-page", renamed);
    const entries = useCommandBarContextStore.getState().entries;
    expect(entries.map((entry) => entry.token)).toEqual(["project-page", "issue-page"]);
    expect(entries[1]!.context).toBe(renamed);
  });

  it("drops a registration on unregister", () => {
    const { register, unregister } = useCommandBarContextStore.getState();
    register("issue-page", { kind: "issue", issue });
    unregister("issue-page");
    unregister("never-registered");

    expect(useCommandBarContextStore.getState().entries).toEqual([]);
  });
});

describe("useRecentVisitsStore", () => {
  beforeEach(() => {
    useRecentVisitsStore.setState({ byWorkspace: {} });
  });

  it("keeps visits per workspace, newest first, without duplicates", () => {
    const { recordVisit } = useRecentVisitsStore.getState();
    recordVisit("ws-a", { type: "project", id: "p1" });
    recordVisit("ws-a", { type: "agent", id: "a1" });
    recordVisit("ws-a", { type: "project", id: "p1" });
    recordVisit("ws-b", { type: "autopilot", id: "ap1" });

    const state = useRecentVisitsStore.getState();
    expect(selectRecentVisits("ws-a")(state).map((e) => `${e.type}:${e.id}`)).toEqual([
      "project:p1",
      "agent:a1",
    ]);
    expect(selectRecentVisits("ws-b")(state)).toHaveLength(1);
    expect(selectRecentVisits(null)(state)).toEqual([]);
  });

  it("treats the same id under another type as a different visit", () => {
    const { recordVisit } = useRecentVisitsStore.getState();
    recordVisit("ws-a", { type: "project", id: "x" });
    recordVisit("ws-a", { type: "agent", id: "x" });

    expect(useRecentVisitsStore.getState().byWorkspace["ws-a"]).toHaveLength(2);
  });

  it("prunes workspaces the user no longer belongs to", () => {
    const { recordVisit, pruneWorkspaces } = useRecentVisitsStore.getState();
    recordVisit("ws-a", { type: "project", id: "p1" });
    recordVisit("ws-gone", { type: "project", id: "p2" });
    pruneWorkspaces(["ws-a"]);

    expect(Object.keys(useRecentVisitsStore.getState().byWorkspace)).toEqual(["ws-a"]);
  });
});
