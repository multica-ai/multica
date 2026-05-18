import { describe, it, expect } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { flattenReplies, isUserQuestionComment } from "./comment-card";

function reply(id: string, parentId: string, createdAt: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "u1",
    content: "",
    parent_id: parentId,
    created_at: createdAt,
    updated_at: createdAt,
    comment_type: "comment",
    reactions: [],
    attachments: [],
  };
}

function buildIndex(entries: TimelineEntry[]): Map<string, TimelineEntry[]> {
  const m = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (!e.parent_id) continue;
    const list = m.get(e.parent_id) ?? [];
    list.push(e);
    m.set(e.parent_id, list);
  }
  return m;
}

describe("flattenReplies", () => {
  it("returns empty array when there are no replies", () => {
    expect(flattenReplies("root", new Map())).toEqual([]);
  });

  it("returns direct children in chronological order", () => {
    const entries = [
      reply("a", "root", "2026-05-01T10:00:00+02:00"),
      reply("b", "root", "2026-05-01T10:05:00+02:00"),
      reply("c", "root", "2026-05-01T10:02:00+02:00"),
    ];
    const flat = flattenReplies("root", buildIndex(entries));
    expect(flat.map((e) => e.id)).toEqual(["a", "c", "b"]);
  });

  it("places a later nested reply after an earlier sibling regardless of tree depth", () => {
    // Tree:
    //   root
    //     A (10:00)
    //       A1 (11:00)
    //         A1a (12:00)   <- deepest, latest
    //     B (10:30)         <- earlier than A1, A1a but appears later in tree-walk
    //
    // Depth-first alone yields [A, A1, A1a, B] — scrambled.
    // Chronological sort yields [A, B, A1, A1a].
    const entries = [
      reply("A", "root", "2026-05-01T10:00:00+02:00"),
      reply("A1", "A", "2026-05-01T11:00:00+02:00"),
      reply("A1a", "A1", "2026-05-01T12:00:00+02:00"),
      reply("B", "root", "2026-05-01T10:30:00+02:00"),
    ];
    const flat = flattenReplies("root", buildIndex(entries));
    expect(flat.map((e) => e.id)).toEqual(["A", "B", "A1", "A1a"]);
  });

  it("matches the production repro: thread under 8a848add is fully chronological", () => {
    // Sub-set extracted from JEH-345 repro data — the tree shape that produced
    // the visible bug on prod.
    const entries = [
      reply("26d9ce8d", "8a848add", "2026-05-01T16:48:12+02:00"),
      reply("2f014a02", "26d9ce8d", "2026-05-01T16:50:38+02:00"),
      reply("f4d3b766", "8a848add", "2026-05-01T16:53:21+02:00"),
      reply("1c93a3da", "f4d3b766", "2026-05-01T16:53:48+02:00"),
      reply("54d16316", "1c93a3da", "2026-05-01T16:55:10+02:00"),
      reply("4ed2dd47", "8a848add", "2026-05-01T16:55:52+02:00"),
      reply("a79924f3", "4ed2dd47", "2026-05-01T16:56:39+02:00"),
      reply("e9ac1906", "a79924f3", "2026-05-01T16:57:22+02:00"),
      reply("abad1575", "54d16316", "2026-05-01T16:59:01+02:00"),
      reply("2f5b3f66", "abad1575", "2026-05-01T17:00:37+02:00"),
      reply("eb15d2f9", "abad1575", "2026-05-01T17:13:16+02:00"),
      reply("6ea86cba", "eb15d2f9", "2026-05-01T17:13:38+02:00"),
    ];
    const flat = flattenReplies("8a848add", buildIndex(entries));
    const timestamps = flat.map((e) => e.created_at);
    const sorted = [...timestamps].sort();
    expect(timestamps).toEqual(sorted);
  });
});

describe("isUserQuestionComment", () => {
  it("only treats question comment entries as user-question prompts", () => {
    expect(isUserQuestionComment({ type: "comment", comment_type: "question" })).toBe(true);
    expect(isUserQuestionComment({ type: "comment", comment_type: "comment" })).toBe(false);
    expect(isUserQuestionComment({ type: "activity", comment_type: "question" })).toBe(false);
  });
});
