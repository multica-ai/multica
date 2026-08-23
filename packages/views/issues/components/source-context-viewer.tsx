"use client";

import { useEffect, useState } from "react";
import { AlertTriangle, ChevronRight, GitBranch, LocateFixed, Maximize2, Minimize2, XIcon } from "lucide-react";
import { issueStatusCategory } from "@multica/core/issues";
import type { Issue, IssueSourceContext, SourceContextCommentSnapshot } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { ProgressRing } from "./progress-ring";
import { SourceContextContent } from "./source-context-content";
import { StatusIcon } from "./status-icon";

function authorLabel(
  context: IssueSourceContext,
  type: string,
  id: string,
  fallback: string,
  labels: {
    unavailable: (capturedName: string) => string;
    renamed: (capturedName: string, currentName: string) => string;
    archived: string;
    deletedAgent: string;
    noLongerInWorkspace: string;
  },
): string {
  const state = context.source_author_state?.find((item) => item.type === type && item.id === id);
  if (!state || state.state === "unavailable") return labels.unavailable(fallback);
  const renamed = state.current_name && state.current_name !== fallback
    ? labels.renamed(fallback, state.current_name)
    : "";
  switch (state.state) {
    case "archived": return `${renamed || fallback} · ${labels.archived}`;
    case "deleted_agent": return `${fallback} · ${labels.deletedAgent}`;
    case "no_longer_in_workspace": return `${fallback} · ${labels.noLongerInWorkspace}`;
    case "renamed": return renamed || fallback;
    default: return fallback;
  }
}

export function SourceContextBadge({
  context,
  refetchIssue,
  parentIssue,
  parentProgress,
}: {
  context: IssueSourceContext;
  refetchIssue: () => Promise<Issue | null | undefined>;
  parentIssue?: Issue | null;
  parentProgress?: { done: number; total: number };
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const [viewerOpen, setViewerOpen] = useState(false);
  const [viewerExpanded, setViewerExpanded] = useState(false);
  const [issueAttachmentDetailsOpen, setIssueAttachmentDetailsOpen] = useState(false);
  const [current, setCurrent] = useState(context);
  useEffect(() => {
    setCurrent(context);
    setIssueAttachmentDetailsOpen(false);
  }, [context]);

  const openCurrent = (value: IssueSourceContext) => {
    if (value.anchor_comment_state !== "available" || !value.current_source || value.can_open_current_source !== true) return;
    navigation.push(`${paths.issueDetail(value.current_source.issue_id)}#comment-${value.current_source.anchor_comment_id}`);
  };
  const refresh = async () => {
    try {
      const fresh = (await refetchIssue())?.source_context;
      if (fresh) setCurrent(fresh);
      return fresh ?? null;
    } catch {
      // Navigation must revalidate live state, but the immutable snapshot is
      // still useful when that network read fails. Callers fall back to the
      // viewer instead of producing an unhandled click promise.
      return null;
    }
  };
  const snapshot = current.snapshot;
  const issueAttachmentChanges = current.change_details?.description_attachment_changes ?? [];
  const addedIssueAttachments = issueAttachmentChanges.filter((change) => change.kind === "added");
  const replacedIssueAttachments = issueAttachmentChanges.filter((change) => change.kind === "replaced");
  const removedIssueAttachments = issueAttachmentChanges.filter((change) => change.kind === "removed");
  const recognizedIssueAttachmentChangeCount = addedIssueAttachments.length
    + replacedIssueAttachments.length
    + removedIssueAttachments.length;
  const issueAttachmentSummary = [
    addedIssueAttachments.length > 0
      ? t(($) => $.source_context.issue_attachment_added_count, { count: addedIssueAttachments.length })
      : null,
    replacedIssueAttachments.length > 0
      ? t(($) => $.source_context.issue_attachment_replaced_count, { count: replacedIssueAttachments.length })
      : null,
    removedIssueAttachments.length > 0
      ? t(($) => $.source_context.issue_attachment_removed_count, { count: removedIssueAttachments.length })
      : null,
  ].filter((part): part is string => part !== null).join(" · ");
  const changeSubjects = (current.change_reasons ?? []).flatMap((reason): string[] => {
    switch (reason) {
      case "issue_title": return [t(($) => $.source_context.change_subject_issue_title)];
      case "issue_description": return [t(($) => $.source_context.change_subject_issue_description)];
      case "issue_description_attachments": return recognizedIssueAttachmentChangeCount > 0
        ? []
        : [t(($) => $.source_context.change_subject_issue_attachments)];
      case "comment_thread": return [t(($) => $.source_context.change_subject_comment_thread)];
      default: return [];
    }
  });
  const sourceStateLabel = current.source_issue_state === "deleted"
    ? t(($) => $.source_context.issue_deleted)
    : current.display_state === "unavailable"
      ? t(($) => $.source_context.summary_unavailable)
      : current.display_state !== "unchanged"
        ? t(($) => $.source_context.summary_changed)
        : null;
  const sourceStateTone = current.source_issue_state === "deleted"
    ? "text-destructive"
    : "text-warning";
  const sourceStateMessage = current.source_issue_state === "deleted"
    ? t(($) => $.source_context.issue_deleted_after_capture)
    : current.display_state === "unavailable"
      ? t(($) => $.source_context.source_check_failed)
      : t(($) => $.source_context.source_changed);
  const sourceStateSentence = t(($) => $.source_context.change_sentence, { content: sourceStateMessage });
  const changedSubjects = changeSubjects.join(" · ");
  const showKnownChanges = current.source_issue_state !== "deleted";
  const changePrefix = current.display_state === "unavailable"
    ? t(($) => $.source_context.confirmed_changes)
    : t(($) => $.source_context.changed);
  const originIdentifier = parentIssue?.identifier ?? snapshot.source_issue.identifier;
  const originTitle = parentIssue?.title ?? snapshot.source_issue.title;

  return (
    <>
      <div
        data-slot="source-context-summary"
        className="mt-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border border-border/60 bg-muted/30 px-2.5 py-1.5 text-caption"
      >
        <div className="flex min-w-0 flex-1 items-center gap-1.5">
          <GitBranch className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="shrink-0 text-muted-foreground">
            {t(($) => $.source_context.branched_from)}
          </span>
          {parentIssue ? (
            <AppLink
              href={paths.issueDetail(parentIssue.id)}
              aria-label={`${originIdentifier} ${originTitle}`}
              className="group/source-parent flex min-w-0 items-center gap-1.5 rounded-sm font-medium text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <StatusIcon
                status={parentIssue.status}
                category={issueStatusCategory(parentIssue) ?? undefined}
                className="size-3.5"
              />
              <span className="shrink-0 tabular-nums" translate="no">{originIdentifier}</span>
              <span className="truncate text-muted-foreground transition-colors group-hover/source-parent:text-foreground">
                {originTitle}
              </span>
            </AppLink>
          ) : (
            <span className="flex min-w-0 items-center gap-1.5 font-medium text-foreground">
              <span className="shrink-0 tabular-nums" translate="no">{originIdentifier}</span>
              <span className="truncate text-muted-foreground">{originTitle}</span>
            </span>
          )}
          {parentProgress && parentProgress.total > 0 && (
            <span className="inline-flex shrink-0 items-center gap-1 text-muted-foreground">
              <ProgressRing done={parentProgress.done} total={parentProgress.total} size={11} />
              <span className="tabular-nums text-micro font-medium">
                {parentProgress.done}/{parentProgress.total}
              </span>
            </span>
          )}
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {sourceStateLabel && (
            <span className={cn("text-micro font-medium", sourceStateTone)} role="status" aria-live="polite">
              {sourceStateLabel}
            </span>
          )}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 gap-1 px-2 text-muted-foreground hover:text-foreground"
            onClick={() => setViewerOpen(true)}
          >
            {t(($) => $.source_context.context_snapshot)}
            <ChevronRight className="size-3.5" aria-hidden />
          </Button>
        </div>
      </div>
      <Dialog open={viewerOpen} onOpenChange={setViewerOpen}>
        <DialogContent
          showCloseButton={false}
          className={cn(
            "flex w-full max-w-[calc(100%-2rem)] flex-col gap-0 overflow-hidden p-0 transition-[height,max-width] motion-reduce:transition-none",
            viewerExpanded
              ? "h-[calc(100dvh-2rem)] sm:max-w-[calc(100%-2rem)]"
              : "h-[min(82dvh,56rem)] sm:max-w-4xl",
          )}
        >
          <DialogHeader className="relative shrink-0 gap-1 border-b px-5 py-4 pr-20 sm:px-6">
            <DialogTitle className="text-pretty">{t(($) => $.source_context.viewer_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.source_context.agent_received)} · {new Intl.DateTimeFormat(locale, {
                dateStyle: "medium",
                timeStyle: "short",
              }).format(new Date(current.captured_at))}
            </DialogDescription>
            <div className="absolute right-3 top-3 flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => viewerExpanded
                  ? $.source_context.viewer_restore
                  : $.source_context.viewer_expand)}
                title={t(($) => viewerExpanded
                  ? $.source_context.viewer_restore
                  : $.source_context.viewer_expand)}
                aria-pressed={viewerExpanded}
                onClick={() => setViewerExpanded((value) => !value)}
              >
                {viewerExpanded
                  ? <Minimize2 className="size-4" aria-hidden />
                  : <Maximize2 className="size-4" aria-hidden />}
              </Button>
              {current.anchor_comment_state === "available"
                && current.can_open_current_source === true
                && current.current_source && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.source_context.open_current)}
                  title={t(($) => $.source_context.open_current)}
                  onClick={() => {
                    void refresh().then((fresh) => {
                      if (fresh) openCurrent(fresh);
                    });
                  }}
                >
                  <LocateFixed className="size-4" aria-hidden />
                </Button>
              )}
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.source_context.viewer_close)}
                title={t(($) => $.source_context.viewer_close)}
                onClick={() => setViewerOpen(false)}
              >
                <XIcon className="size-4" aria-hidden />
              </Button>
            </div>
          </DialogHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto overscroll-contain px-5 py-5 sm:px-6">
            {current.display_state !== "unchanged" && (
              <div
                data-slot="source-context-change-summary"
                className="flex gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-caption"
              >
                <AlertTriangle className="size-4 shrink-0" aria-hidden />
                <div className="min-w-0 flex-1 space-y-1.5">
                  <p className="break-words font-medium" role="status" aria-live="polite">
                    {sourceStateSentence}
                    {showKnownChanges && changedSubjects && (
                      <span className="font-normal text-muted-foreground">
                        {t(($) => $.source_context.label_spacing)}
                        <span className="font-medium text-foreground">{changePrefix}</span>
                        {t(($) => $.source_context.label_spacing)}
                        {changedSubjects}
                      </span>
                    )}
                  </p>
                  {showKnownChanges && issueAttachmentSummary && (
                    <p
                      data-slot="source-context-issue-attachment-summary"
                      className="min-w-0 break-words text-muted-foreground"
                    >
                      <span className="font-medium text-foreground">
                        {t(($) => $.source_context.issue_attachments)}
                      </span>
                      {t(($) => $.source_context.label_spacing)}
                      {issueAttachmentSummary}
                      {recognizedIssueAttachmentChangeCount > 0 && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          className="-my-1 ml-1 size-6 align-middle text-muted-foreground hover:text-foreground"
                          aria-label={t(($) => issueAttachmentDetailsOpen
                            ? $.source_context.hide_issue_attachment_details
                            : $.source_context.show_issue_attachment_details)}
                          title={t(($) => issueAttachmentDetailsOpen
                            ? $.source_context.hide_issue_attachment_details
                            : $.source_context.show_issue_attachment_details)}
                          aria-expanded={issueAttachmentDetailsOpen}
                          onClick={() => setIssueAttachmentDetailsOpen((value) => !value)}
                        >
                          <ChevronRight
                            className={cn(
                              "size-3.5 transition-transform motion-reduce:transition-none",
                              issueAttachmentDetailsOpen && "rotate-90",
                            )}
                            aria-hidden
                          />
                        </Button>
                      )}
                    </p>
                  )}
                  {current.display_state === "unavailable" && (
                    <p className="text-muted-foreground">
                      {t(($) => $.source_context.captured_context_available)}
                    </p>
                  )}
                  {showKnownChanges && recognizedIssueAttachmentChangeCount > 0 && issueAttachmentDetailsOpen && (
                    <ul className="space-y-1 pl-5 text-muted-foreground">
                      {addedIssueAttachments.length > 0 && (
                        <li className="list-disc break-words">
                          <span className="font-medium text-foreground">
                            {t(($) => $.source_context.issue_attachment_added_label)}
                          </span>
                          {t(($) => $.source_context.label_spacing)}
                          <span translate="no">
                            {addedIssueAttachments.map((change) => change.filename).join(", ")}
                          </span>
                        </li>
                      )}
                      {replacedIssueAttachments.length > 0 && (
                        <li className="list-disc break-words">
                          <span className="font-medium text-foreground">
                            {t(($) => $.source_context.issue_attachment_replaced_label)}
                          </span>
                          {t(($) => $.source_context.label_spacing)}
                          <span translate="no">
                            {replacedIssueAttachments.map((change) => (
                              `${change.previous_filename ?? change.filename} → ${change.filename}`
                            )).join(", ")}
                          </span>
                        </li>
                      )}
                      {removedIssueAttachments.length > 0 && (
                        <li className="list-disc break-words">
                          <span className="font-medium text-foreground">
                            {t(($) => $.source_context.issue_attachment_removed_label)}
                          </span>
                          {t(($) => $.source_context.label_spacing)}
                          <span translate="no">
                            {removedIssueAttachments.map((change) => change.filename).join(", ")}
                          </span>
                        </li>
                      )}
                    </ul>
                  )}
                </div>
              </div>
            )}
            <SourceContextContent
              sourceIssue={snapshot.source_issue}
              comments={snapshot.comment_thread}
              anchorCommentId={snapshot.anchor_comment_id}
              changedCommentIds={current.change_details?.changed_comment_ids}
              addedComments={current.change_details?.added_comments}
              removedCommentIds={current.change_details?.removed_comment_ids}
              issueTitleSuffix={current.current_source?.identifier && current.current_source.identifier !== snapshot.source_issue.identifier
                ? ` · ${t(($) => $.source_context.identifier_now, { identifier: current.current_source.identifier })}`
                : undefined}
              getAuthorLabel={(comment: SourceContextCommentSnapshot) => authorLabel(
                current,
                comment.author.type,
                comment.author.id,
                comment.author.name,
                {
                  unavailable: (capturedName) => t(($) => $.source_context.author_at_capture, { name: capturedName }),
                  renamed: (capturedName, currentName) => t(($) => $.source_context.author_renamed, { captured: capturedName, current: currentName }),
                  archived: t(($) => $.source_context.author_archived),
                  deletedAgent: t(($) => $.source_context.author_deleted_agent),
                  noLongerInWorkspace: t(($) => $.source_context.author_left_workspace),
                },
              )}
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
