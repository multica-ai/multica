import type { ReplyTarget } from "@/data/stores/reply-target-store";

/** Convert the active target into the preview and focus key consumed by the composer. */
export function getReplyTargetComposerProps(target: ReplyTarget | null) {
  return {
    replyTarget: target
      ? { actorName: target.actorName, preview: target.preview }
      : null,
    expandTrigger: target?.commentId ?? null,
  };
}
