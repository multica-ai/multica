// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import {
  findCommentFocusAnchor,
  resolveCommentFocusTarget,
} from "./comment-focus";

describe("comment execution-log focus target", () => {
  const itemIds = ["root-a", "root-d", "root-g"];
  const replyToRoot = new Map([
    ["reply-b", "root-a"],
    ["reply-c", "root-a"],
    ["reply-f", "root-d"],
  ]);

  it("uses a root comment's own virtualized row", () => {
    expect(resolveCommentFocusTarget("root-d", itemIds, replyToRoot)).toEqual({
      rootId: "root-d",
      index: 1,
    });
  });

  it("maps a nested reply to its root-owned virtualized row", () => {
    expect(resolveCommentFocusTarget("reply-f", itemIds, replyToRoot)).toEqual({
      rootId: "root-d",
      index: 1,
    });
  });

  it("rejects a deleted or stale comment id", () => {
    expect(resolveCommentFocusTarget("missing", itemIds, replyToRoot)).toBeNull();
  });

  it("lands on the exact comment header inside a thread wrapper", () => {
    const wrapper = document.createElement("div");
    wrapper.innerHTML = `
      <div data-comment-focus-anchor="root-d">root header</div>
      <div data-comment-focus-anchor="reply-f">reply header</div>
    `;

    expect(findCommentFocusAnchor(wrapper, "reply-f").textContent).toBe(
      "reply header",
    );
  });

  it("falls back to the wrapper for legacy markup", () => {
    const wrapper = document.createElement("div");
    expect(findCommentFocusAnchor(wrapper, "root-d")).toBe(wrapper);
  });
});
