import { beforeEach, describe, expect, it } from "vitest";
import { useCommentCollapseStore } from "./comment-collapse-store";

beforeEach(() => {
  useCommentCollapseStore.setState({ collapsedByIssue: {} });
});

describe("useCommentCollapseStore.setIssueCommentsCollapsed", () => {
  it("collapses every provided comment id for an issue", () => {
    useCommentCollapseStore
      .getState()
      .setIssueCommentsCollapsed("issue-1", ["comment-1", "comment-2", "comment-1"], true);

    expect(useCommentCollapseStore.getState().collapsedByIssue["issue-1"]).toEqual([
      "comment-1",
      "comment-2",
    ]);
  });

  it("expands only the provided comment ids for the target issue", () => {
    useCommentCollapseStore.setState({
      collapsedByIssue: {
        "issue-1": ["comment-1", "comment-2", "comment-3"],
        "issue-2": ["comment-1"],
      },
    });

    useCommentCollapseStore
      .getState()
      .setIssueCommentsCollapsed("issue-1", ["comment-1", "comment-3"], false);

    expect(useCommentCollapseStore.getState().collapsedByIssue).toEqual({
      "issue-1": ["comment-2"],
      "issue-2": ["comment-1"],
    });
  });

  it("removes the issue bucket when every collapsed comment is expanded", () => {
    useCommentCollapseStore.setState({
      collapsedByIssue: {
        "issue-1": ["comment-1", "comment-2"],
      },
    });

    useCommentCollapseStore
      .getState()
      .setIssueCommentsCollapsed("issue-1", ["comment-1", "comment-2"], false);

    expect(useCommentCollapseStore.getState().collapsedByIssue["issue-1"]).toBeUndefined();
  });
});
