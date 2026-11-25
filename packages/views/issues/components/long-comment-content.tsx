"use client";

import { useCallback } from "react";
import { ChevronDown, Download } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";
import { selectExpandedLongComments, useLongCommentExpandStore } from "@multica/core/issues/stores";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { PLAIN_TEXT_COMMENT_CHARS, isLongComment, longCommentPreview } from "../utils/long-comment";

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
  const toggle = useCallback(
    () => setExpanded(issueId, commentId, !expanded),
    [setExpanded, issueId, commentId, expanded],
  );

  if (!long) return <ReadonlyContent content={content} attachments={attachments} />;

  const plainText = expanded && content.length > PLAIN_TEXT_COMMENT_CHARS;
  return (
    <div data-long-comment={expanded ? "expanded" : "collapsed"}>
      {plainText ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
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
        <div
          className={cn(
            !expanded && "[mask-image:linear-gradient(to_bottom,black_60%,transparent)]",
          )}
        >
          <ReadonlyContent content={expanded ? content : longCommentPreview(content)} attachments={attachments} />
        </div>
      )}
      <button type="button" onClick={toggle} aria-expanded={expanded} className={TOGGLE_CLASS}>
        <ChevronDown
          className={cn(
            "h-3 w-3 shrink-0 transition-transform duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none",
            expanded && "rotate-180",
          )}
        />
        <span>{expanded ? t(($) => $.comment.long.collapse) : t(($) => $.comment.long.expand)}</span>
      </button>
    </div>
  );
}
