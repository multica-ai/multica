"use client";

import type { ReactNode } from "react";
import type {
  SourceContextCommentSnapshot,
  SourceContextIssueSnapshot,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor";
import { SourceContextCommentList } from "./source-context-comment-list";

/**
 * Shared presentation for a source issue snapshot and its captured comment
 * thread history. Callers own freshness/status messaging; this component owns
 * the content hierarchy and spacing so preview and persisted capture surfaces
 * cannot drift visually.
 */
export function SourceContextContent({
  sourceIssue,
  comments,
  anchorCommentId,
  issueTitleSuffix,
  getAuthorLabel,
  changedCommentIds,
  addedComments,
  removedCommentIds,
  className,
}: {
  sourceIssue: SourceContextIssueSnapshot;
  comments: SourceContextCommentSnapshot[];
  anchorCommentId: string;
  issueTitleSuffix?: ReactNode;
  getAuthorLabel?: (comment: SourceContextCommentSnapshot) => string;
  changedCommentIds?: string[];
  addedComments?: SourceContextCommentSnapshot[];
  removedCommentIds?: string[];
  className?: string;
}) {
  return (
    <div
      data-slot="source-context-content"
      className={cn("space-y-8", className)}
    >
      <section className="space-y-4">
        <h3 className="break-words text-body font-semibold text-pretty">
          {sourceIssue.identifier} · {sourceIssue.title}
          {issueTitleSuffix}
        </h3>
        {sourceIssue.description && (
          <ReadonlyContent content={sourceIssue.description} />
        )}
      </section>
      <SourceContextCommentList
        comments={comments}
        anchorCommentId={anchorCommentId}
        getAuthorLabel={getAuthorLabel}
        changedCommentIds={changedCommentIds}
        addedComments={addedComments}
        removedCommentIds={removedCommentIds}
      />
    </div>
  );
}
