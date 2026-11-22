// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  rankAssigneePicks,
  selectAssigneePicks,
  useAssigneePicksStore,
  type AssigneePick,
} from "./assignee-picks-store";

const STORAGE_KEY = "multica_assignee_picks";
const picks = (userId: string, wsId: string) =>
  selectAssigneePicks(userId, wsId)(useAssigneePicksStore.getState());

beforeEach(() => {
  localStorage.clear();
  useAssigneePicksStore.setState({ byScope: {} });
});

describe("useAssigneePicksStore.recordPick", () => {
  it("appends every pick, repeats included", () => {
    const { recordPick } = useAssigneePicksStore.getState();
    recordPick("u1", "ws1", { type: "member", id: "m1" });
    recordPick("u1", "ws1", { type: "agent", id: "a1" });
    recordPick("u1", "ws1", { type: "member", id: "m1" });

    expect(picks("u1", "ws1")).toEqual([
      { type: "member", id: "m1" },
      { type: "agent", id: "a1" },
      { type: "member", id: "m1" },
    ]);
  });

  it("drops the oldest pick when the 101st arrives", () => {
    const { recordPick } = useAssigneePicksStore.getState();
    recordPick("u1", "ws1", { type: "squad", id: "oldest" });
    for (let i = 0; i < 100; i++) recordPick("u1", "ws1", { type: "agent", id: "a1" });

    const queue = picks("u1", "ws1");
    expect(queue).toHaveLength(100);
    expect(rankAssigneePicks(queue)).toEqual([{ type: "agent", id: "a1", count: 100 }]);
    const persisted = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}");
    expect(persisted.state.byScope["u1:ws1"]).toHaveLength(100);
  });

  it("keeps each account and workspace separate", () => {
    const { recordPick } = useAssigneePicksStore.getState();
    recordPick("u1", "ws1", { type: "member", id: "m1" });
    recordPick("u1", "ws2", { type: "member", id: "m2" });
    recordPick("u2", "ws1", { type: "member", id: "m3" });

    expect(picks("u1", "ws1").map((p) => p.id)).toEqual(["m1"]);
    expect(picks("u1", "ws2").map((p) => p.id)).toEqual(["m2"]);
    expect(picks("u2", "ws1").map((p) => p.id)).toEqual(["m3"]);
  });

  it("survives a reload from storage", async () => {
    useAssigneePicksStore.getState().recordPick("u1", "ws1", { type: "agent", id: "a1" });
    const saved = localStorage.getItem(STORAGE_KEY)!;
    // A fresh page: empty memory, the saved queue still on disk.
    useAssigneePicksStore.setState({ byScope: {} });
    localStorage.setItem(STORAGE_KEY, saved);

    await useAssigneePicksStore.persist.rehydrate();

    expect(picks("u1", "ws1")).toEqual([{ type: "agent", id: "a1" }]);
  });

  it("evicts the least recently used scope past 20", () => {
    const { recordPick } = useAssigneePicksStore.getState();
    for (let i = 0; i < 20; i++) recordPick("u1", `ws${i}`, { type: "member", id: "m1" });
    recordPick("u1", "ws0", { type: "member", id: "m1" });
    recordPick("u1", "ws-new", { type: "member", id: "m1" });

    const scopes = Object.keys(useAssigneePicksStore.getState().byScope);
    expect(scopes).toHaveLength(20);
    expect(scopes).toContain("u1:ws0");
    expect(scopes).not.toContain("u1:ws1");
  });
});

describe("rankAssigneePicks", () => {
  it("orders by count, breaking ties by the most recent pick", () => {
    const queue: AssigneePick[] = [
      { type: "member", id: "m1" },
      { type: "agent", id: "a1" },
      { type: "squad", id: "s1" },
      { type: "agent", id: "a1" },
      { type: "member", id: "m1" },
    ];
    expect(rankAssigneePicks(queue)).toEqual([
      { type: "member", id: "m1", count: 2 },
      { type: "agent", id: "a1", count: 2 },
      { type: "squad", id: "s1", count: 1 },
    ]);
  });
});
