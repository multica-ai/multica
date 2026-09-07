"use client";

import type { SourceContextCommentSnapshot } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";

export function SourceContextCommentList({
  comments,
  anchorCommentId,
  getAuthorLabel,
  changedCommentIds,
  addedComments,
  removedCommentIds,
  className,
}: {
  comments: SourceContextCommentSnapshot[];
  anchorCommentId: string;
  getAuthorLabel?: (comment: SourceContextCommentSnapshot) => string;
  changedCommentIds?: string[];
  addedComments?: SourceContextCommentSnapshot[];
  removedCommentIds?: string[];
  className?: string;
}) {
  const { t } = useT("issues");
  const changedComments = new Set(changedCommentIds ?? []);
  const removedComments = new Set(removedCommentIds ?? []);
  const capturedCommentIds = new Set(comments.map((comment) => comment.id));
  const addedOnly = (addedComments ?? []).filter((comment) => !capturedCommentIds.has(comment.id));
  const addedCommentIds = new Set(addedOnly.map((comment) => comment.id));
  const displayComments = [...comments, ...addedOnly].sort((left, right) => {
    if (left.created_at !== right.created_at) return left.created_at < right.created_at ? -1 : 1;
    if (left.id === right.id) return 0;
    return left.id < right.id ? -1 : 1;
  });

  return (
    <ol
      className={cn("space-y-3", className)}
      aria-label={t(($) => $.source_context.thread_history)}
    >
      {displayComments.map((comment) => {
        const changeKind = removedComments.has(comment.id)
          ? "deleted"
          : addedCommentIds.has(comment.id)
            ? "added"
            : changedComments.has(comment.id)
              ? "changed"
              : null;
        const changeLabel = changeKind === "added"
          ? t(($) => $.source_context.comment_change_added)
          : changeKind === "changed"
            ? t(($) => $.source_context.comment_change_changed)
            : changeKind === "deleted"
              ? t(($) => $.source_context.comment_change_deleted)
              : null;
        return (
          <li
            key={comment.id}
            className="border-l-2 pl-3 transition-colors duration-150 hover:border-l-foreground/50"
          >
            <div
              className={cn(
                changeKind && "-my-1 -mr-2 rounded-md px-2 py-1.5",
                changeKind === "added" && "bg-success/5",
                changeKind === "changed" && "bg-amber-500/5",
                changeKind === "deleted" && "bg-destructive/5",
              )}
              data-source-context-change-kind={changeKind ?? undefined}
            >
              <div className="flex min-w-0 items-center gap-2 text-caption font-medium text-muted-foreground">
                <span className="truncate">
                  {addedCommentIds.has(comment.id)
                    ? comment.author.name
                    : getAuthorLabel?.(comment) ?? comment.author.name}
                </span>
                {comment.id === anchorCommentId && (
                  <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-info">
                    {t(($) => $.source_context.source_comment)}
                  </span>
                )}
                {changeLabel && <span className="sr-only">{changeLabel}</span>}
              </div>
              <div className="mt-1.5 break-words">
                <ReadonlyContent content={comment.content} />
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
