import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { collectThreadReplies } from "./thread-utils";

function comment(id: string, createdAt: string, parentId: string | null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: id,
    parent_id: parentId,
    created_at: createdAt,
    updated_at: createdAt,
    comment_type: "comment",
  } as TimelineEntry;
}

function bucketByParent(entries: TimelineEntry[]): Map<string, TimelineEntry[]> {
  const map = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (!e.parent_id) continue;
    const list = map.get(e.parent_id) ?? [];
    list.push(e);
    map.set(e.parent_id, list);
  }
  return map;
}

describe("collectThreadReplies", () => {
  it("orders a late nested reply after earlier sibling replies (#3691)", () => {
    // R1 (50m ago) triggered a slow agent; R2 (30m) and R3 (10m) arrived while
    // it ran; D (3m ago) is the agent's reply, forced to nest under R1. A
    // depth-first walk yields R1-D-R2-R3; the thread must read R1-R2-R3-D.
    const r1 = comment("r1", "2026-06-11T10:00:00Z", "root");
    const r2 = comment("r2", "2026-06-11T10:20:00Z", "root");
    const r3 = comment("r3", "2026-06-11T10:40:00Z", "root");
    const d = comment("d", "2026-06-11T10:47:00Z", "r1");

    const out = collectThreadReplies("root", bucketByParent([r1, r2, r3, d]));

    expect(out.map((e) => e.id)).toEqual(["r1", "r2", "r3", "d"]);
  });

  it("still returns every descendant across nesting levels", () => {
    const r1 = comment("r1", "2026-06-11T10:00:00Z", "root");
    const d1 = comment("d1", "2026-06-11T10:05:00Z", "r1");
    const d2 = comment("d2", "2026-06-11T10:10:00Z", "d1");

    const out = collectThreadReplies("root", bucketByParent([r1, d1, d2]));

    expect(out.map((e) => e.id)).toEqual(["r1", "d1", "d2"]);
  });

  it("breaks created_at ties by id so the order is deterministic", () => {
    const b = comment("b", "2026-06-11T10:00:00Z", "root");
    const a = comment("a", "2026-06-11T10:00:00Z", "b");

    const out = collectThreadReplies("root", bucketByParent([b, a]));

    expect(out.map((e) => e.id)).toEqual(["a", "b"]);
  });
});
