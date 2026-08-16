import type { TimelineEntry } from "@multica/core/types";
import { describe, expect, it } from "vitest";

import {
  buildCommentUpdateBody,
  commentRevisionFromTimeline,
  shouldAcceptServerRevision,
  withCachedIssueRevision,
} from "./revision";

describe("mobile revision requests", () => {
  it("adds the cached issue revision without overriding an explicit revision", () => {
    expect(withCachedIssueRevision({ title: "Latest" }, 4)).toEqual({
      title: "Latest",
      expected_revision: 4,
    });
    expect(
      withCachedIssueRevision({ title: "Retry", expected_revision: 7 }, 8),
    ).toEqual({ title: "Retry", expected_revision: 7 });
  });

  it("reads the edited comment revision and serializes it", () => {
    const timeline = [
      {
        type: "comment",
        id: "comment-1",
        revision: 6,
      } as TimelineEntry,
    ];

    expect(commentRevisionFromTimeline(timeline, "comment-1")).toBe(6);
    expect(buildCommentUpdateBody("Latest", ["attachment-1"], 6)).toEqual({
      content: "Latest",
      attachment_ids: ["attachment-1"],
      expected_revision: 6,
    });
  });

  it("does not let an older HTTP response overwrite a newer cache revision", () => {
    expect(shouldAcceptServerRevision(5, 4)).toBe(false);
    expect(shouldAcceptServerRevision(5, 5)).toBe(false);
    expect(shouldAcceptServerRevision(5, 6)).toBe(true);
    expect(shouldAcceptServerRevision(undefined, undefined)).toBe(true);
  });
});
