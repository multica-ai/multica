// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import { useCommentShareStore } from "./comment-share-store";

describe("useCommentShareStore", () => {
  beforeEach(() => useCommentShareStore.getState().clear());

  it("starts on an issue with the picked thread", () => {
    useCommentShareStore.getState().start("issue-1", "c1");
    expect(useCommentShareStore.getState()).toMatchObject({ issueId: "issue-1", selected: ["c1"] });
  });

  it("keeps the pick order and toggles threads off", () => {
    const s = useCommentShareStore.getState();
    s.start("issue-1", "c2");
    s.toggle("c1");
    s.toggle("c3");
    s.toggle("c2");
    expect(useCommentShareStore.getState().selected).toEqual(["c1", "c3"]);
  });

  it("adds to the selection when started again on the same issue", () => {
    const s = useCommentShareStore.getState();
    s.start("issue-1", "c1");
    s.start("issue-1", "c2");
    s.start("issue-1", "c1");
    expect(useCommentShareStore.getState().selected).toEqual(["c1", "c2"]);
  });

  it("drops another issue's selection", () => {
    const s = useCommentShareStore.getState();
    s.start("issue-1", "c1");
    s.start("issue-2");
    expect(useCommentShareStore.getState()).toMatchObject({ issueId: "issue-2", selected: [] });
  });
});
