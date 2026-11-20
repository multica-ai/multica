import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  compareAssigneeUsage,
  selectAssigneeUsage,
  useAssigneeUsageStore,
} from "./assignee-usage-store";

beforeEach(() => {
  useAssigneeUsageStore.setState({ byWorkspace: {} });
  vi.useFakeTimers();
  vi.setSystemTime(1_000);
});

afterEach(() => {
  vi.useRealTimers();
});

const ranked = (wsId: string) =>
  [...selectAssigneeUsage(wsId)(useAssigneeUsageStore.getState())]
    .sort(compareAssigneeUsage)
    .map((e) => `${e.type}:${e.id}:${e.count}`);

describe("useAssigneeUsageStore.recordAssignment", () => {
  it("counts repeat picks and ranks the most-used first", () => {
    const { recordAssignment } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "u1" });
    recordAssignment("ws-a", { type: "agent", id: "a1" });
    recordAssignment("ws-a", { type: "agent", id: "a1" });
    recordAssignment("ws-a", { type: "squad", id: "s1" });
    recordAssignment("ws-a", { type: "agent", id: "a1" });
    recordAssignment("ws-a", { type: "squad", id: "s1" });

    expect(ranked("ws-a")).toEqual(["agent:a1:3", "squad:s1:2", "member:u1:1"]);
  });

  it("breaks count ties in favour of the more recent pick", () => {
    const { recordAssignment } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "u1" });
    vi.setSystemTime(2_000);
    recordAssignment("ws-a", { type: "member", id: "u2" });

    expect(ranked("ws-a")).toEqual(["member:u2:1", "member:u1:1"]);
  });

  it("treats the same id under different assignee types as different entries", () => {
    const { recordAssignment } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "x" });
    recordAssignment("ws-a", { type: "agent", id: "x" });

    expect(ranked("ws-a")).toHaveLength(2);
  });

  it("keeps counts namespaced by workspace id", () => {
    const { recordAssignment } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "u1" });
    recordAssignment("ws-b", { type: "member", id: "u2" });

    expect(ranked("ws-a")).toEqual(["member:u1:1"]);
    expect(ranked("ws-b")).toEqual(["member:u2:1"]);
    expect(ranked("ws-c")).toEqual([]);
  });

  it("caps a workspace bucket and drops the least-used entries", () => {
    const { recordAssignment } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "keep" });
    recordAssignment("ws-a", { type: "member", id: "keep" });
    for (let i = 0; i < 40; i++) {
      vi.setSystemTime(2_000 + i);
      recordAssignment("ws-a", { type: "agent", id: `a${i}` });
    }

    const entries = useAssigneeUsageStore.getState().byWorkspace["ws-a"] ?? [];
    expect(entries).toHaveLength(30);
    expect(entries.some((e) => e.id === "keep")).toBe(true);
    expect(entries.some((e) => e.id === "a39")).toBe(true);
    expect(entries.some((e) => e.id === "a0")).toBe(false);
  });
});

describe("useAssigneeUsageStore.pruneWorkspaces", () => {
  it("drops buckets for workspaces the user no longer belongs to", () => {
    const { recordAssignment, pruneWorkspaces } = useAssigneeUsageStore.getState();
    recordAssignment("ws-a", { type: "member", id: "u1" });
    recordAssignment("ws-b", { type: "member", id: "u2" });

    pruneWorkspaces(["ws-a"]);

    expect(Object.keys(useAssigneeUsageStore.getState().byWorkspace)).toEqual(["ws-a"]);
  });
});
