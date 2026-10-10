// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { getReplyTargetComposerProps } from "./reply-target-composer";

describe("reply target composer props", () => {
  afterEach(() => useReplyTargetStore.getState().clear());

  it("updates the reply preview and focus trigger when the target changes", () => {
    useReplyTargetStore.getState().setTarget({
      commentId: "comment-old",
      actorName: "Old author",
      preview: "Old comment preview",
    });
    expect(
      getReplyTargetComposerProps(useReplyTargetStore.getState().target),
    ).toEqual({
      replyTarget: { actorName: "Old author", preview: "Old comment preview" },
      expandTrigger: "comment-old",
    });

    useReplyTargetStore.getState().setTarget({
      commentId: "comment-new",
      actorName: "New author",
      preview: "New comment preview",
    });
    expect(
      getReplyTargetComposerProps(useReplyTargetStore.getState().target),
    ).toEqual({
      replyTarget: { actorName: "New author", preview: "New comment preview" },
      expandTrigger: "comment-new",
    });
  });
});
