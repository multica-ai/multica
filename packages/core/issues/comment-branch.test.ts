import { describe, expect, it } from "vitest";
import type { AgentTask } from "../types";
import {
  commentBranchPointsByTaskId,
  ensureCommentBranchRequest,
  shouldRetainCommentBranchRequest,
} from "./comment-branch";

describe("commentBranchPointsByTaskId", () => {
  it("indexes only independent comment-branch tasks", () => {
    const tasks = [
      { id: "branch-task", branch_point_comment_id: "source-comment" },
      { id: "ordinary-task" },
    ] as AgentTask[];

    expect([...commentBranchPointsByTaskId(tasks)]).toEqual([
      ["branch-task", "source-comment"],
    ]);
  });

  it("omits provenance links whose source comment was deleted", () => {
    const tasks = [
      { id: "present", branch_point_comment_id: "source-comment" },
      { id: "deleted", branch_point_comment_id: "deleted-comment" },
    ] as AgentTask[];

    expect([
      ...commentBranchPointsByTaskId(
        tasks,
        new Set(["source-comment"]),
      ),
    ]).toEqual([["present", "source-comment"]]);
  });
});

describe("comment branch idempotency intent", () => {
  it("reuses a key only while the exact request intent is unresolved", () => {
    let sequence = 0;
    const createRequestId = () => `request-${++sequence}`;
    const automatic = {
      commentId: "comment-1",
      contentBase: "frozen content",
    };

    const first = ensureCommentBranchRequest(
      null,
      automatic,
      createRequestId,
    );
    expect(
      ensureCommentBranchRequest(first, automatic, createRequestId).requestId,
    ).toBe("request-1");
    expect(
      ensureCommentBranchRequest(
        first,
        { ...automatic, agentId: "agent-1" },
        createRequestId,
      ).requestId,
    ).toBe("request-2");
  });

  it("retains the key only for ambiguous transport and server failures", () => {
    expect(shouldRetainCommentBranchRequest()).toBe(true);
    expect(shouldRetainCommentBranchRequest(500)).toBe(true);
    expect(shouldRetainCommentBranchRequest(503)).toBe(true);
    expect(shouldRetainCommentBranchRequest(400)).toBe(false);
    expect(shouldRetainCommentBranchRequest(409)).toBe(false);
    expect(shouldRetainCommentBranchRequest(422)).toBe(false);
  });
});
