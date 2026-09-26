// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "../types";
import {
  applyCommentDeletion,
  commentLandingTarget,
  isDeletedComment,
  removeCommentSubtree,
} from "./comment-deletion";

const DELETED_AT = "2026-09-11T08:00:00Z";

function comment(id: string, parentId: string | null, extra: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: `body ${id}`,
    parent_id: parentId,
    comment_type: "comment",
    reactions: [],
    attachments: [],
    created_at: "2026-09-11T07:00:00Z",
    updated_at: "2026-09-11T07:00:00Z",
    ...extra,
  };
}

const ids = (entries: TimelineEntry[]) => entries.map((e) => e.id);

describe("isDeletedComment", () => {
  it.each([
    [DELETED_AT, true],
    ["", false],
    [null, false],
    [undefined, false],
  ])("deleted_at %j → %s", (deletedAt, expected) => {
    expect(isDeletedComment({ deleted_at: deletedAt })).toBe(expected);
  });
});

describe("applyCommentDeletion", () => {
  it("tombstones a comment that has replies and keeps every reply", () => {
    // A → B → C, plus A → D.
    const timeline = [
      comment("a", null),
      comment("b", "a", {
        reactions: [{ id: "r", comment_id: "b", actor_type: "member", actor_id: "u", emoji: "👍", created_at: DELETED_AT }],
        resolved_at: DELETED_AT,
        resolved_by_type: "member",
        resolved_by_id: "u",
      }),
      comment("c", "b"),
      comment("d", "a"),
    ];

    const next = applyCommentDeletion(timeline, "b", DELETED_AT);

    expect(ids(next)).toEqual(["a", "b", "c", "d"]);
    expect(next[1]).toMatchObject({
      content: "",
      reactions: [],
      attachments: [],
      resolved_at: null,
      deleted_at: DELETED_AT,
    });
    expect(next[2]).toBe(timeline[2]);
  });

  it("removes a comment without replies and leaves a live parent alone", () => {
    const timeline = [comment("a", null), comment("b", "a")];
    expect(applyCommentDeletion(timeline, "b", DELETED_AT)).toEqual([timeline[0]]);
  });

  it("prunes every tombstone ancestor left without replies", () => {
    const timeline = [
      comment("a", null, { deleted_at: DELETED_AT, content: "" }),
      comment("b", "a", { deleted_at: DELETED_AT, content: "" }),
      comment("c", "b"),
      comment("other", null),
    ];
    expect(ids(applyCommentDeletion(timeline, "c", DELETED_AT))).toEqual(["other"]);
  });

  it("stops at a tombstone that still holds another reply", () => {
    const timeline = [
      comment("a", null, { deleted_at: DELETED_AT, content: "" }),
      comment("b", "a", { deleted_at: DELETED_AT, content: "" }),
      comment("c", "b"),
      comment("d", "a"),
    ];
    expect(ids(applyCommentDeletion(timeline, "c", DELETED_AT))).toEqual(["a", "d"]);
  });
});

describe("removeCommentSubtree", () => {
  it("removes the comment and every descendant, and nothing else", () => {
    const timeline = [comment("a", null), comment("b", "a"), comment("c", "b"), comment("d", null)];
    expect(ids(removeCommentSubtree(timeline, "a"))).toEqual(["d"]);
  });
});

describe("commentLandingTarget", () => {
  const tombstone = (id: string) => comment(id, "root", { content: "", deleted_at: DELETED_AT });
  const live = (id: string) => comment(id, "root");

  it("lands on the visible reply above a tombstone", () => {
    const replies = [live("r1"), tombstone("r2"), live("r3")];
    expect(commentLandingTarget("r2", "root", replies)).toBe("r1");
  });

  it("skips back over consecutive tombstones", () => {
    const replies = [live("r1"), tombstone("r2"), tombstone("r3")];
    expect(commentLandingTarget("r3", "root", replies)).toBe("r1");
  });

  it("falls back to the thread root when nothing visible precedes the tombstone", () => {
    expect(commentLandingTarget("r1", "root", [tombstone("r1"), live("r2")])).toBe("root");
  });

  it.each([
    ["a live reply", "r2", [live("r1"), live("r2")]],
    ["an id the thread does not hold — a root, or a comment outside the cache", "root", [live("r1")]],
  ])("leaves %s as its own target", (_case, target, replies: TimelineEntry[]) => {
    expect(commentLandingTarget(target, "root", replies)).toBe(target);
  });
});
