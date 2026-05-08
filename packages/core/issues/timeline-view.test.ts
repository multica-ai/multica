import { describe, it, expect } from "vitest";
import type { TimelineEntry } from "../types";
import { buildTimelineGroups } from "./timeline-view";

// All test fixtures use ISO timestamps because the coalesce comparison parses
// `created_at` via Date(). Build entries chronologically ascending — that's
// the contract `buildTimelineGroups` documents.
const t = (mins: number) =>
  new Date(2026, 0, 1, 12, mins).toISOString();

function comment(
  id: string,
  parentId: string | null,
  minute: number,
  actorId = "user-1",
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: actorId,
    created_at: t(minute),
    parent_id: parentId,
    content: id,
  };
}

function activity(
  id: string,
  action: string,
  minute: number,
  actorId = "user-1",
): TimelineEntry {
  return {
    type: "activity",
    id,
    actor_type: "member",
    actor_id: actorId,
    created_at: t(minute),
    action,
    details: {},
  };
}

describe("buildTimelineGroups", () => {
  it("collects a 3-level reply chain (A -> B -> C -> D) into A's group", () => {
    const entries = [
      comment("A", null, 0),
      comment("B", "A", 1),
      comment("C", "B", 2),
      comment("D", "C", 3),
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups).toHaveLength(1);
    const g = groups[0]!;
    expect(g.kind).toBe("comment");
    if (g.kind !== "comment") return;
    expect(g.parent.id).toBe("A");
    expect(g.replies.map((r) => r.id)).toEqual(["B", "C", "D"]);
  });

  it("preserves DFS preorder for branching reply trees (A -> [B->C, D, E->F])", () => {
    const entries = [
      comment("A", null, 0),
      comment("B", "A", 1),
      comment("C", "B", 2),
      comment("D", "A", 3),
      comment("E", "A", 4),
      comment("F", "E", 5),
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups).toHaveLength(1);
    const g = groups[0]!;
    if (g.kind !== "comment") throw new Error("unreachable");
    // DFS: B, then B's subtree (C), then D, then E, then E's subtree (F).
    expect(g.replies.map((r) => r.id)).toEqual(["B", "C", "D", "E", "F"]);
  });

  it("coalesces 4 consecutive same-actor status_changed within 2 min into one entry", () => {
    const entries = [
      activity("a1", "status_changed", 0),
      activity("a2", "status_changed", 0.5),
      activity("a3", "status_changed", 1),
      activity("a4", "status_changed", 1.5),
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups).toHaveLength(1);
    const g = groups[0]!;
    if (g.kind !== "activities") throw new Error("unreachable");
    expect(g.entries).toHaveLength(1);
    // Coalesce keeps the LAST entry's identity (its action, timestamp), bumps count.
    expect(g.entries[0]!.id).toBe("a4");
    expect(g.entries[0]!.coalesced_count).toBe(4);
  });

  it("coalesces task_completed across any time gap (no window limit)", () => {
    const entries = [
      activity("t1", "task_completed", 0),
      activity("t2", "task_completed", 30), // 30 min later — outside 2-min window
      activity("t3", "task_completed", 120), // 2 hours later
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups).toHaveLength(1);
    const g = groups[0]!;
    if (g.kind !== "activities") throw new Error("unreachable");
    expect(g.entries).toHaveLength(1);
    expect(g.entries[0]!.coalesced_count).toBe(3);
  });

  it("does NOT coalesce non-whitelisted actions across the 2-min window", () => {
    const entries = [
      activity("a1", "status_changed", 0),
      activity("a2", "status_changed", 5), // 5 min apart — outside window
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups).toHaveLength(1);
    const g = groups[0]!;
    if (g.kind !== "activities") throw new Error("unreachable");
    expect(g.entries).toHaveLength(2);
    expect(g.entries.every((e) => (e.coalesced_count ?? 1) === 1)).toBe(true);
  });

  it("splits mixed activity/comment runs into the correct group sequence", () => {
    const entries = [
      comment("c1", null, 0),
      activity("a1", "status_changed", 1),
      activity("a2", "status_changed", 1.2), // coalesces with a1
      comment("c2", null, 2),
      comment("c3", "c2", 3), // reply to c2
      activity("a3", "priority_changed", 4),
    ];
    const { groups } = buildTimelineGroups(entries);

    expect(groups.map((g) => g.kind)).toEqual([
      "comment",
      "activities",
      "comment",
      "activities",
    ]);

    // c1 — no replies.
    const g0 = groups[0]!;
    if (g0.kind !== "comment") throw new Error("unreachable");
    expect(g0.parent.id).toBe("c1");
    expect(g0.replies).toEqual([]);

    // a1 + a2 coalesced into one entry inside one activities group.
    const g1 = groups[1]!;
    if (g1.kind !== "activities") throw new Error("unreachable");
    expect(g1.entries).toHaveLength(1);
    expect(g1.entries[0]!.coalesced_count).toBe(2);

    // c2 — picks up reply c3.
    const g2 = groups[2]!;
    if (g2.kind !== "comment") throw new Error("unreachable");
    expect(g2.parent.id).toBe("c2");
    expect(g2.replies.map((r) => r.id)).toEqual(["c3"]);

    // a3 alone.
    const g3 = groups[3]!;
    if (g3.kind !== "activities") throw new Error("unreachable");
    expect(g3.entries).toHaveLength(1);
    expect(g3.entries[0]!.id).toBe("a3");
  });

  it("does NOT coalesce activities from different actors", () => {
    const entries = [
      activity("a1", "status_changed", 0, "user-1"),
      activity("a2", "status_changed", 0.5, "user-2"),
    ];
    const { groups } = buildTimelineGroups(entries);

    const g = groups[0]!;
    if (g.kind !== "activities") throw new Error("unreachable");
    expect(g.entries).toHaveLength(2);
  });

  it("returns repliesByParent indexed at one level (for consumers that walk themselves)", () => {
    const entries = [
      comment("A", null, 0),
      comment("B", "A", 1),
      comment("C", "B", 2),
    ];
    const { repliesByParent } = buildTimelineGroups(entries);

    expect(repliesByParent.get("A")?.map((e) => e.id)).toEqual(["B"]);
    expect(repliesByParent.get("B")?.map((e) => e.id)).toEqual(["C"]);
    expect(repliesByParent.get("C")).toBeUndefined();
  });
});
