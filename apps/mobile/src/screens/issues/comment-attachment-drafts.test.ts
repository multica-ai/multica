import { describe, expect, it } from "vitest";
import { createDraftCommentAttachment } from "./comment-attachment-drafts";

describe("createDraftCommentAttachment", () => {
  it("keeps selected comment attachments as local upload assets", () => {
    const draft = createDraftCommentAttachment(
      {
        uri: "file:///tmp/spec.txt",
        name: "spec.txt",
        mimeType: "text/plain",
        size: 42,
      },
      0,
      123,
    );

    expect(draft).toEqual({
      id: "file:///tmp/spec.txt:spec.txt:42:123:0",
      uri: "file:///tmp/spec.txt",
      name: "spec.txt",
      mimeType: "text/plain",
      size: 42,
    });
    expect("issue_id" in draft).toBe(false);
    expect("comment_id" in draft).toBe(false);
  });
});
