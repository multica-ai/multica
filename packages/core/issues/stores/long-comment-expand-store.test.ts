import { beforeEach, describe, expect, it } from "vitest";
import { selectExpandedLongComments, useLongCommentExpandStore } from "./long-comment-expand-store";

describe("long comment expand store", () => {
  beforeEach(() => {
    useLongCommentExpandStore.setState({ expandedByIssue: {} });
  });

  it("tracks expanded comments per issue", () => {
    const { setExpanded } = useLongCommentExpandStore.getState();

    setExpanded("issue-1", "c1", true);
    setExpanded("issue-1", "c2", true);
    setExpanded("issue-2", "c9", true);
    setExpanded("issue-1", "c1", false);

    const state = useLongCommentExpandStore.getState();
    expect([...selectExpandedLongComments("issue-1")(state)]).toEqual(["c2"]);
    expect([...selectExpandedLongComments("issue-2")(state)]).toEqual(["c9"]);
  });

  it("drops the issue key with its last id and keeps references stable on no-ops", () => {
    const { setExpanded } = useLongCommentExpandStore.getState();
    const empty = selectExpandedLongComments("issue-1")(useLongCommentExpandStore.getState());

    setExpanded("issue-1", "c1", true);
    const expanded = useLongCommentExpandStore.getState().expandedByIssue;
    setExpanded("issue-1", "c1", true);
    expect(useLongCommentExpandStore.getState().expandedByIssue).toBe(expanded);

    setExpanded("issue-1", "c1", false);
    expect(useLongCommentExpandStore.getState().expandedByIssue).toEqual({});
    expect(selectExpandedLongComments("issue-1")(useLongCommentExpandStore.getState())).toBe(empty);
  });
});
