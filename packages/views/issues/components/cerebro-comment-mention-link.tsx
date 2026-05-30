"use client";

// CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 — renders a `mention://comment/<id>`
// breadcrumb as an in-issue "jump to the new thread" link. The move-to-thread
// backend writes this mention into the breadcrumb it leaves on each moved
// comment; here it resolves to a scroll-to-comment on the current issue.

import { ArrowUpRight } from "lucide-react";
import { useT } from "../../i18n";

/**
 * Scroll the issue timeline to the comment with the given id and flash it.
 * Comments are anchored as `#comment-<id>` in issue-detail.tsx and
 * comment-card.tsx, so the same anchor that the inbox deep-link uses works
 * for an in-page jump.
 */
function jumpToComment(commentId: string): void {
  const el = document.getElementById(`comment-${commentId}`);
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "start" });
  // Brief pulse so the eye lands on the right thread. Mirrors the ring used by
  // the inbox highlight path; removed after the animation so it doesn't stick.
  el.classList.add("ring-2", "ring-brand/50", "bg-brand/5", "transition-colors", "duration-700");
  window.setTimeout(() => {
    el.classList.remove("ring-2", "ring-brand/50", "bg-brand/5");
  }, 1600);
}

export function CommentMentionLink({ commentId }: { commentId: string }) {
  const { t } = useT("issues");
  return (
    <button
      type="button"
      onClick={() => jumpToComment(commentId)}
      className="inline-flex items-center gap-0.5 rounded font-medium text-brand underline-offset-2 hover:underline"
    >
      {t(($) => $.comment.move_thread.breadcrumb)}
      <ArrowUpRight className="h-3.5 w-3.5" />
    </button>
  );
}
