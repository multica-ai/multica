import type { TimelineEntry } from "@multica/core/types";
import { describe, expect, it } from "vitest";

import {
  buildCommentUpdateBody,
  commentContentFromTimeline,
  shouldAcceptServerRevision,
} from "./revision";

describe("mobile revision requests", () => {
  it("reads the edited comment content and serializes it as the field baseline", () => {
    const timeline = [
      {
        type: "comment",
        id: "comment-1",
        content: "Original",
      } as TimelineEntry,
    ];

    expect(commentContentFromTimeline(timeline, "comment-1")).toBe("Original");
    expect(buildCommentUpdateBody("Latest", ["attachment-1"], "Original")).toEqual({
      content: "Latest",
      attachment_ids: ["attachment-1"],
      content_base: "Original",
    });
  });

  it("does not let an older HTTP response overwrite a newer cache revision", () => {
    expect(shouldAcceptServerRevision(5, 4)).toBe(false);
    expect(shouldAcceptServerRevision(5, 5)).toBe(false);
    expect(shouldAcceptServerRevision(5, 6)).toBe(true);
    expect(shouldAcceptServerRevision(undefined, undefined)).toBe(true);
  });
});
