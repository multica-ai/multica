import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  collectThreadReplies,
  resolvedThreadRootIds,
  rootCommentIds,
} from "./thread-utils";

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

function activity(id: string, createdAt: string): TimelineEntry {
  return {
    type: "activity",
    id,
    actor_type: "member",
    actor_id: "user-1",
    action: "status_changed",
    created_at: createdAt,
  } as TimelineEntry;
}

describe("rootCommentIds", () => {
  it("returns top-level comments only, skipping replies and activities", () => {
    const entries = [
      activity("act-1", "2026-06-11T09:00:00Z"),
      comment("root-1", "2026-06-11T10:00:00Z", null),
      comment("reply-1", "2026-06-11T10:05:00Z", "root-1"),
      comment("root-2", "2026-06-11T11:00:00Z", null),
    ];

    expect(rootCommentIds(entries)).toEqual(["root-1", "root-2"]);
  });
});

describe("resolvedThreadRootIds", () => {
  it("includes root-resolved and reply-resolved threads, excludes unresolved", () => {
    const rootResolved = {
      ...comment("root-resolved", "2026-06-11T10:00:00Z", null),
      resolved_at: "2026-06-11T12:00:00Z",
    };
    const replyResolvedRoot = comment("root-reply-resolved", "2026-06-11T10:10:00Z", null);
    const resolutionReply = {
      ...comment("reply-resolution", "2026-06-11T10:20:00Z", "root-reply-resolved"),
      resolved_at: "2026-06-11T12:30:00Z",
    };
    const openRoot = comment("root-open", "2026-06-11T10:30:00Z", null);
    const openReply = comment("reply-open", "2026-06-11T10:40:00Z", "root-open");

    const ids = resolvedThreadRootIds([
      activity("act-1", "2026-06-11T09:00:00Z"),
      rootResolved,
      replyResolvedRoot,
      resolutionReply,
      openRoot,
      openReply,
    ]);

    expect(ids).toEqual(["root-resolved", "root-reply-resolved"]);
  });

  it("detects a resolution on a nested reply", () => {
    const root = comment("root", "2026-06-11T10:00:00Z", null);
    const reply = comment("reply", "2026-06-11T10:05:00Z", "root");
    const nested = {
      ...comment("nested", "2026-06-11T10:10:00Z", "reply"),
      resolved_at: "2026-06-11T12:00:00Z",
    };

    expect(resolvedThreadRootIds([root, reply, nested])).toEqual(["root"]);
  });
});
