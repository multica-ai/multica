"use client";

import { useState } from "react";
import { ChevronRight, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { SourceContextPreview } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import { SourceContextContent } from "../issues/components/source-context-content";
import { useT } from "../i18n";

function sourceContextErrorCode(error: unknown): unknown {
  if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return undefined;
  return (error.body as { code?: unknown }).code;
}

function isDeletedSourceError(error: unknown): boolean {
  const code = sourceContextErrorCode(error);
  return code === "anchor_comment_deleted" || code === "source_issue_deleted";
}

export function useSourceContextFailureMessage() {
  const { t } = useT("issues");
  return (error: unknown): string | null => {
    if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return null;
    const body = error.body as {
      code?: unknown;
      limits?: {
        comment_count?: unknown;
        text_bytes?: unknown;
        attachment_count?: unknown;
        attachment_bytes?: unknown;
      };
    };
    if (body.code === "anchor_comment_deleted") {
      return t(($) => $.source_context.preview_anchor_deleted);
    }
    if (body.code === "source_issue_deleted") {
      return t(($) => $.source_context.preview_issue_deleted);
    }
    if (body.code !== "source_context_too_large" || !body.limits) return null;
    const messages: string[] = [];
    if (typeof body.limits.comment_count === "number" && body.limits.comment_count > 256) {
      messages.push(t(($) => $.source_context.error_comment_limit, { count: body.limits!.comment_count }));
    }
    if (typeof body.limits.text_bytes === "number" && body.limits.text_bytes > 1024 * 1024) {
      messages.push(t(($) => $.source_context.error_text_limit, { size: body.limits!.text_bytes }));
    }
    if (typeof body.limits.attachment_count === "number" && body.limits.attachment_count > 100) {
      messages.push(t(($) => $.source_context.error_attachment_count_limit, { count: body.limits!.attachment_count }));
    }
    if (typeof body.limits.attachment_bytes === "number" && body.limits.attachment_bytes > 500 * 1024 * 1024) {
      messages.push(t(($) => $.source_context.error_attachment_size_limit, { size: body.limits!.attachment_bytes }));
    }
    return messages.join(" ") || t(($) => $.source_context.error_too_large);
  };
}

export function SourceContextPreviewCard({
  preview,
  loading,
  failed,
  error,
  onRetry,
  constrainToParent = false,
  expanded: controlledExpanded,
  onExpandedChange,
}: {
  preview?: SourceContextPreview;
  loading?: boolean;
  failed?: boolean;
  error?: unknown;
  onRetry?: () => void;
  /** Keep an expanded preview inside its parent's flex content area; the
   *  owning dialog decides whether that expansion also changes its height. */
  constrainToParent?: boolean;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
}) {
  const { t } = useT("issues");
  const sourceContextFailureMessage = useSourceContextFailureMessage();
  const [internalExpanded, setInternalExpanded] = useState(false);
  const expanded = controlledExpanded ?? internalExpanded;
  const toggleExpanded = () => {
    const next = !expanded;
    if (controlledExpanded === undefined) setInternalExpanded(next);
    onExpandedChange?.(next);
  };

  if (loading) {
    return (
      <div className="mx-4 mb-2 flex shrink-0 items-center gap-2 rounded-md border px-3 py-2 text-caption text-muted-foreground" role="status">
        <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" aria-hidden />
        {t(($) => $.source_context.preview_loading)}
      </div>
    );
  }
  if (!preview || failed) {
    const retryAvailable = onRetry && !isDeletedSourceError(error);
    return (
      <div className="mx-4 mb-2 flex shrink-0 items-center justify-between gap-3 rounded-md border border-destructive/40 px-3 py-2 text-caption" role="alert">
        <span className="min-w-0 break-words">{sourceContextFailureMessage(error) ?? t(($) => $.source_context.preview_failed)}</span>
        {retryAvailable && (
          <Button type="button" variant="ghost" size="sm" onClick={onRetry}>
            <RefreshCw className="size-3.5" aria-hidden />
            {t(($) => $.source_context.refresh)}
          </Button>
        )}
      </div>
    );
  }

  const anchor = preview.comment_thread.find((comment) => comment.id === preview.anchor_comment_id);
  return (
    <div
      data-slot="source-context-preview"
      className={cn(
        "mx-4 mb-2 shrink-0 overflow-hidden rounded-md border bg-muted/25",
        constrainToParent && (expanded
          ? "flex h-[53%] min-h-0 shrink-0 flex-col"
          : "shrink-0"),
      )}
    >
      <button
        type="button"
        className="flex w-full min-w-0 shrink-0 items-center gap-2 px-3 py-2 text-left text-caption hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        aria-expanded={expanded}
        onClick={toggleExpanded}
      >
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform motion-reduce:transition-none", expanded && "rotate-90")} aria-hidden />
        <span className="min-w-0 truncate font-medium">
          {t(($) => $.source_context.preview_summary, {
            identifier: preview.source_issue.identifier,
            count: preview.comment_thread.length,
          })}
        </span>
        {anchor?.author.name && (
          <span className="truncate text-muted-foreground">· {anchor.author.name}</span>
        )}
      </button>
      {expanded && (
        <div
          data-slot="source-context-preview-body"
          className={cn(
            "border-t px-3 py-3",
            constrainToParent
              ? "min-h-0 flex-1 overflow-y-auto overscroll-contain"
              : "max-h-72 overflow-y-auto overscroll-contain",
          )}
        >
          <SourceContextContent
            sourceIssue={preview.source_issue}
            comments={preview.comment_thread}
            anchorCommentId={preview.anchor_comment_id}
          />
        </div>
      )}
    </div>
  );
}
