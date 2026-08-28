// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { buildTimelineRows } from "./timeline-thread";
import {
  buildCommentDirectory,
  filterCommentDirectory,
} from "./comment-directory";

function comment(
  id: string,
  created_at: string,
  extra: Partial<TimelineEntry> = {},
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: `user-${id}`,
    content: `body of ${id}`,
    parent_id: null,
    created_at,
    updated_at: created_at,
    comment_type: "comment",
    reactions: [],
    attachments: [],
    ...extra,
  } as TimelineEntry;
}

function activity(id: string, created_at: string): TimelineEntry {
  return {
    type: "activity",
    id,
    created_at,
    actor_type: "member",
    actor_id: "user-x",
  } as unknown as TimelineEntry;
}

const entries: TimelineEntry[] = [
  activity("act-1", "2026-01-01T00:00:00Z"),
  comment("root-2", "2026-01-02T00:00:00Z"),
  comment("root-1-reply", "2026-01-03T00:00:00Z", {
    parent_id: "root-1",
    content: "a reply mentioning deploy",
  }),
  comment("root-1", "2026-01-01T12:00:00Z", { content: "first root" }),
];

describe("buildCommentDirectory", () => {
  it("lists only root comments, in ASC (oldest-first) order", () => {
    const rows = buildTimelineRows(entries);
    const dir = buildCommentDirectory(rows);
    expect(dir.map((d) => d.rootId)).toEqual(["root-1", "root-2"]);
  });

  it("carries reply count, author ref and summary from the root row", () => {
    const rows = buildTimelineRows(entries);
    const dir = buildCommentDirectory(rows);
    expect(dir[0]).toMatchObject({
      rootId: "root-1",
      replyCount: 1,
      authorType: "member",
      authorId: "user-root-1",
      summary: "first root",
      createdAt: "2026-01-01T12:00:00Z",
    });
  });

  it("includes orphan replies promoted to top-level by buildTimelineRows", () => {
    // A reply whose parent was deleted (or is missing from the loaded
    // batch) is rescued into a top-level row by buildTimelineRows. The
    // directory must surface it — dropping it would hide a comment the
    // timeline itself renders (Counts-agree parity rule).
    const rows = buildTimelineRows([
      comment("orphan-reply", "2026-01-04T00:00:00Z", {
        parent_id: "deleted-root",
        content: "rescued orphan",
      }),
      comment("root-1", "2026-01-01T00:00:00Z"),
    ]);
    const dir = buildCommentDirectory(rows);
    expect(dir.map((d) => d.rootId)).toEqual(["root-1", "orphan-reply"]);
    const orphan = dir.find((d) => d.rootId === "orphan-reply");
    expect(orphan).toMatchObject({
      authorType: "member",
      authorId: "user-orphan-reply",
      summary: "rescued orphan",
      replyCount: 0,
      resolved: false,
    });
  });

  it("matches on raw root content even when the summary truncated it away", () => {
    const rows = buildTimelineRows([
      comment("root-1", "2026-01-01T00:00:00Z", {
        content: `${"x".repeat(130)} deploy`,
      }),
      comment("root-2", "2026-01-02T00:00:00Z"),
    ]);
    const dir = buildCommentDirectory(rows);
    // "deploy" sits past the 120-cp summary cap → summary misses it, the
    // raw-content fallback must still match root-1 only.
    const hits = filterCommentDirectory(dir, "deploy");
    expect(hits.map((h) => h.rootId)).toEqual(["root-1"]);
  });
});

describe("filterCommentDirectory", () => {
  const rows = buildTimelineRows(entries);
  const dir = buildCommentDirectory(rows);

  it("empty / whitespace query returns the list untouched, order preserved", () => {
    expect(filterCommentDirectory(dir, "")).toEqual(dir);
    expect(filterCommentDirectory(dir, "   ")).toEqual(dir);
  });

  it("case-insensitive substring match on author name or summary", () => {
    const names = { "root-1": "Alice", "root-2": "Bob" };
    expect(
      filterCommentDirectory(dir, "ALICE", names).map((d) => d.rootId),
    ).toEqual(["root-1"]);
    expect(filterCommentDirectory(dir, "first root").map((d) => d.rootId)).toEqual([
      "root-1",
    ]);
  });

  it("no match yields an empty list", () => {
    expect(filterCommentDirectory(dir, "zzz-not-there")).toEqual([]);
  });
});
