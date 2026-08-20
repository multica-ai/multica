import { describe, expect, it } from "vitest";
import {
  CommentBranchResponseSchema,
  ServerCapabilitiesSchema,
} from "./schemas";

describe("mobile comment branch schemas", () => {
  it("accepts a valid branch response and rejects missing identities", () => {
    expect(
      CommentBranchResponseSchema.parse({
        task: { id: "task-1", status: "queued" },
        branch_point_comment_id: "comment-1",
        source_task_id: null,
      }),
    ).toMatchObject({
      task: { id: "task-1", status: "queued" },
      branch_point_comment_id: "comment-1",
    });
    expect(() =>
      CommentBranchResponseSchema.parse({
        task: {},
        branch_point_comment_id: "",
      }),
    ).toThrow();
  });

  it("keeps capability negotiation disabled when the field is absent", () => {
    expect(ServerCapabilitiesSchema.parse({})).toEqual({
      comment_branch_v1: false,
    });
    expect(() =>
      ServerCapabilitiesSchema.parse({ comment_branch_v1: "yes" }),
    ).toThrow();
  });
});
