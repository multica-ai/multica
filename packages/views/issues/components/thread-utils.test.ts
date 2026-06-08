import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  collectThreadReplies,
  partitionThreadReplies,
  THREAD_REPLY_COLLAPSE_THRESHOLD,
} from "./thread-utils";

function entry(id: string, parentId?: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: id,
    created_at: "2026-01-01T00:00:00.000Z",
    parent_id: parentId,
  };
}

function makeReplies(count: number): TimelineEntry[] {
  return Array.from({ length: count }, (_, i) => entry(`r${i + 1}`));
}

describe("partitionThreadReplies", () => {
  it("returns all replies when the thread is short", () => {
    const replies = makeReplies(THREAD_REPLY_COLLAPSE_THRESHOLD);
    const result = partitionThreadReplies(replies, false);
    expect(result.kind).toBe("all");
    if (result.kind === "all") {
      expect(result.replies).toHaveLength(THREAD_REPLY_COLLAPSE_THRESHOLD);
    }
  });

  it("folds the middle when the thread is long and not expanded", () => {
    const replies = makeReplies(10);
    const result = partitionThreadReplies(replies, false);
    expect(result.kind).toBe("collapsed");
    if (result.kind === "collapsed") {
      expect(result.head.map((r) => r.id)).toEqual(["r1", "r2"]);
      expect(result.tail.map((r) => r.id)).toEqual(["r9", "r10"]);
      expect(result.hidden.map((r) => r.id)).toEqual([
        "r3",
        "r4",
        "r5",
        "r6",
        "r7",
        "r8",
      ]);
      expect(result.hiddenCount).toBe(6);
    }
  });

  it("shows everything when expanded", () => {
    const replies = makeReplies(10);
    const result = partitionThreadReplies(replies, true);
    expect(result.kind).toBe("all");
    if (result.kind === "all") {
      expect(result.replies).toHaveLength(10);
    }
  });

  it("shows everything when folding is disabled", () => {
    const replies = makeReplies(10);
    const result = partitionThreadReplies(replies, false, { enabled: false });
    expect(result.kind).toBe("all");
  });
});

describe("collectThreadReplies", () => {
  it("walks nested replies in traversal order", () => {
    const repliesByParent = new Map<string, TimelineEntry[]>([
      ["root", [entry("r1", "root")]],
      ["r1", [entry("r2", "r1")]],
    ]);

    expect(
      collectThreadReplies("root", repliesByParent).map((r) => r.id),
    ).toEqual(["r1", "r2"]);
  });
});
