import { describe, expect, it } from "vitest";
import { CommentSchema, TimelineEntriesSchema } from "./schemas";
const comment = { id: "comment", issue_id: "issue", author_type: "member", author_id: "user", content: "> quote\n\nNote", type: "comment", parent_id: null, created_at: "now", updated_at: "now" };
const anchor = { version: 1, id: "selection", issue_id: "issue", quote: "quote", start: 0, prefix: "", suffix: "", source_text: "quote" };
describe("additive description anchor responses", () => {
  it("accepts old comments and retains all valid selections", () => {
    expect(CommentSchema.parse(comment).description_anchors).toBeUndefined();
    expect(CommentSchema.parse({ ...comment, description_anchors: [anchor] }).description_anchors).toEqual([anchor]);
    expect(TimelineEntriesSchema.parse([{ ...comment, actor_type: "member", actor_id: "user", description_anchors: [anchor] }])[0]?.description_anchors).toEqual([anchor]);
  });
  it("keeps the readable comment when additive metadata is malformed or from a future version", () => {
    for (const value of [null, {}, [{ ...anchor, start: -1 }], [{ ...anchor, version: 2 }], [{ ...anchor, quote: "" }]]) {
      const parsed = CommentSchema.parse({ ...comment, description_anchors: value });
      expect(parsed.content).toBe(comment.content);
      expect(parsed.description_anchors).toBeUndefined();
    }
  });
});
