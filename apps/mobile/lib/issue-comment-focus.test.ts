import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import type { TimelineRow } from "@/lib/timeline-thread";
import {
  findCommentFocusRowIndex,
  getCommentFocusOffset,
} from "./issue-comment-focus";

const comment = (id: string): TimelineEntry =>
  ({ id, type: "comment" }) as TimelineEntry;

describe("findCommentFocusRowIndex", () => {
  const rows: TimelineRow[] = [
    { entry: comment("root-a"), replies: [comment("reply-b")] },
    { entry: comment("root-d"), replies: [comment("reply-f")] },
  ];

  it("finds a root row", () => {
    expect(findCommentFocusRowIndex(rows, "root-d")).toBe(1);
  });

  it("maps a nested reply to its owning root row", () => {
    expect(findCommentFocusRowIndex(rows, "reply-f")).toBe(1);
  });

  it("does not substitute another row for a stale id", () => {
    expect(findCommentFocusRowIndex(rows, "missing")).toBe(-1);
  });
});

describe("getCommentFocusOffset", () => {
  it("does not move a fully visible target", () => {
    expect(
      getCommentFocusOffset({
        currentOffset: 400,
        targetTop: 240,
        targetHeight: 80,
        viewportTop: 100,
        viewportHeight: 500,
      }),
    ).toBeNull();
  });

  it("centers a nested target that is outside the viewport", () => {
    expect(
      getCommentFocusOffset({
        currentOffset: 400,
        targetTop: 700,
        targetHeight: 80,
        viewportTop: 100,
        viewportHeight: 500,
      }),
    ).toBe(790);
  });

  it("aligns an oversized target to the visible top edge", () => {
    expect(
      getCommentFocusOffset({
        currentOffset: 900,
        targetTop: 40,
        targetHeight: 700,
        viewportTop: 100,
        viewportHeight: 500,
      }),
    ).toBe(824);
  });
});
