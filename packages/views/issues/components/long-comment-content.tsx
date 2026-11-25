"use client";

import { useCallback, useLayoutEffect, useRef } from "react";
import { ChevronRight, Download } from "@multica/ui/icons";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { prefersReducedMotion } from "@multica/core/appearance";
import type { Attachment } from "@multica/core/types";
import { selectExpandedLongComments, useLongCommentExpandStore } from "@multica/core/issues/stores";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { isLongComment, isPlainTextComment, longCommentPreview } from "../utils/long-comment";

const TOGGLE_CLASS =
  "mt-1 flex items-center gap-1.5 text-label text-muted-foreground transition-colors hover:text-foreground";

function downloadText(content: string, filename: string) {
  const url = URL.createObjectURL(new Blob([content], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function scrollParent(element: HTMLElement): HTMLElement | null {
  for (let node = element.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(node).overflowY) && node.scrollHeight > node.clientHeight) return node;
  }
  return null;
}

/**
 * Collapsing a body the reader scrolled through would leave the toggle far
 * above the viewport; bring it back to the top edge. Drives the scroll
 * container directly: native scrollIntoView on the issue page also scrolls
 * the desktop shell (#3929). A toggle still in view stays put.
 */
function keepToggleInView(toggle: HTMLElement | null, container: HTMLElement | null) {
  if (!toggle || !container) return;
  const above = container.getBoundingClientRect().top - toggle.getBoundingClientRect().top;
  if (above > 0) container.scrollTop -= above;
}

/**
 * A comment body that opens collapsed when it is long (see
 * `../utils/long-comment`). Short bodies render exactly as before.
 */
export function LongCommentContent({
  issueId,
  commentId,
  content,
  attachments,
}: {
  issueId: string;
  commentId: string;
  content: string;
  attachments?: Attachment[];
}) {
  const { t } = useT("issues");
  const long = isLongComment(content);
  const expanded = useLongCommentExpandStore((s) => long && selectExpandedLongComments(issueId)(s).has(commentId));
  const setExpanded = useLongCommentExpandStore((s) => s.setExpanded);
  const bodyRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  // Height before a toggle the reader clicked; remounts never animate.
  const fromHeightRef = useRef<number | null>(null);
  const toggle = useCallback(() => {
    fromHeightRef.current = bodyRef.current?.getBoundingClientRect().height ?? null;
    setExpanded(issueId, commentId, !expanded);
  }, [setExpanded, issueId, commentId, expanded]);

  useLayoutEffect(() => {
    const from = fromHeightRef.current;
    fromHeightRef.current = null;
    const body = bodyRef.current;
    if (from === null || !body) return;
    const container = scrollParent(body);
    if (!expanded) keepToggleInView(toggleRef.current, container);
    if (prefersReducedMotion() || !body.animate) return;
    const to = body.getBoundingClientRect().height;
    // Shrinking from a body taller than the viewport would only play blank
    // frames after the scroll above; it lands instead.
    if (Math.abs(to - from) < 1 || (!expanded && container && from > container.clientHeight)) return;
    const animation = body.animate(
      [
        { height: `${from}px`, opacity: 0.6, overflow: "hidden" },
        { height: `${to}px`, opacity: 1, overflow: "hidden" },
      ],
      { duration: UI_MOTION_DURATION.standard * 1000, easing: UI_EASE_OUT_CSS },
    );
    return () => animation.cancel();
  }, [expanded]);

  if (!long) return <ReadonlyContent content={content} attachments={attachments} />;

  const plainText = expanded && isPlainTextComment(content);
  const body = plainText ? (
    <div className="flex flex-col gap-2">
      <div data-find-ignore className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
        <span>{t(($) => $.comment.long.plain_text_notice)}</span>
        <button
          type="button"
          onClick={() => downloadText(content, `comment-${commentId.slice(0, 8)}.txt`)}
          className="flex items-center gap-1 transition-colors hover:text-foreground"
        >
          <Download className="h-3 w-3 shrink-0" />
          {t(($) => $.comment.long.download)}
        </button>
      </div>
      <pre
        data-testid="long-comment-plain-text"
        className="max-h-[60vh] overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-caption whitespace-pre-wrap [overflow-wrap:anywhere]"
      >
        {content}
      </pre>
    </div>
  ) : (
    <div className={cn(!expanded && "[mask-image:linear-gradient(to_bottom,black_60%,transparent)]")}>
      <ReadonlyContent content={expanded ? content : longCommentPreview(content)} attachments={attachments} />
    </div>
  );

  return (
    <div data-long-comment={expanded ? "expanded" : "collapsed"}>
      <div ref={bodyRef}>{body}</div>
      <button
        ref={toggleRef}
        type="button"
        onClick={toggle}
        aria-expanded={expanded}
        data-long-comment-toggle
        data-find-ignore
        className={TOGGLE_CLASS}
      >
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 transition-transform duration-200 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
            expanded && "rotate-90",
          )}
        />
        <span>{expanded ? t(($) => $.comment.long.collapse) : t(($) => $.comment.long.expand)}</span>
      </button>
    </div>
  );
}
