"use client";

import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useNeedsMeRepliesStore, type NeedsMeItem } from "@multica/core/home";
import { useWorkspaceId } from "@multica/core/hooks";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
import { useCreateComment, useUpdateIssue } from "@multica/core/issues/mutations";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Issue, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorAvatar } from "../../common/actor-avatar";
import { ReadonlyContent } from "../../editor";
import { useIssueActions } from "../../issues/actions/use-issue-actions";
import { CommentInput } from "../../issues/components/comment-input";
import { AssigneePicker } from "../../issues/components/pickers/assignee-picker";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { NeedsMeKindChip } from "./needs-me-kind-chip";
import { useNeedsMeCopy } from "./use-needs-me-copy";
import { useSourceComment } from "./use-source-comment";

/**
 * One request, handled in place: what is being asked (the source comment,
 * verbatim), a reply box when the answer is words, and the few status moves
 * that finish it. Every move calls an existing endpoint; nothing here adds
 * state to the issue.
 */
export function QueueDetail({
  item,
  replied,
  next,
  onDone,
  onSelectNext,
  leading,
}: {
  item: NeedsMeItem;
  /** The viewer's reply is still the latest thing on the issue. */
  replied: boolean;
  next: NeedsMeItem | null;
  /** The request is finished; move the selection on. */
  onDone: () => void;
  onSelectNext: (key: string) => void;
  leading?: React.ReactNode;
}) {
  const { t } = useT("home");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const copy = useNeedsMeCopy();
  const markReplied = useNeedsMeRepliesStore((s) => s.markReplied);
  const archive = useArchiveInbox();

  const issueId = item.issueId;
  const { data: fetchedIssue } = useQuery({
    ...issueDetailOptions(wsId, issueId ?? ""),
    enabled: !!issueId,
    placeholderData: item.issue ?? undefined,
  });
  const issue: Issue | null = fetchedIssue ?? item.issue ?? null;
  // Reassigning goes through the shared actions so an agent assignment still
  // passes the run-confirm gate; status moves write directly (below).
  const { updateField } = useIssueActions(issue);
  const updateIssue = useUpdateIssue();
  const { comment, isLoading: commentLoading } = useSourceComment(item);
  const [requestingChanges, setRequestingChanges] = useState(false);

  const name = copy.waitingName(item);
  const identifier = issue?.identifier ?? item.identifier ?? "";
  const issueHref = issueId ? paths.issueDetail(identifier || issueId) : null;

  const archiveRelated = () => {
    const id = item.inboxIds[0];
    if (id) archive.mutate(id);
  };

  /**
   * A status move that finishes the request, with an Undo back to where it
   * was. Finishing moves the selection and the optimistic patch drops the
   * entry from the queue, so this pane unmounts before the write settles:
   * results are read off the promise, which resolves regardless, rather than
   * per-call callbacks, which an unmounted observer never fires.
   */
  const moveStatus = (status: IssueStatus, message: string) => {
    if (!issue) return;
    const target = issue;
    const previous = target.status;
    const failed = (err: unknown) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.queue.toast.failed));
    updateIssue
      .mutateAsync({ id: target.id, status })
      .then(() => {
        archiveRelated();
        toast.success(message, {
          action: {
            label: t(($) => $.queue.toast.undo),
            onClick: () => {
              updateIssue.mutateAsync({ id: target.id, status: previous }).catch(failed);
            },
          },
        });
      })
      .catch(failed);
    onDone();
  };

  // Stays on this request: assigning an agent can open the run-confirm dialog,
  // and the entry leaves the queue by itself once it no longer involves you.
  const reassign = (updates: Partial<UpdateIssueRequest>) => {
    updateField(updates, {
      onSuccess: () => toast.success(t(($) => $.queue.toast.reassigned, { identifier })),
    });
  };

  const dismiss = () => {
    archiveRelated();
    toast.success(t(($) => $.queue.toast.dismissed));
    onDone();
  };

  const createComment = useCreateComment(issueId ?? "");
  // Server time of the reply just posted: the mark compares it against the
  // issue's own activity clock, never the browser's.
  const lastReplyAt = useRef<string | null>(null);
  const submitReply = async (
    content: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ): Promise<string | false> => {
    if (!issueId || !content.trim()) return false;
    try {
      const created = await createComment.mutateAsync({ content, attachmentIds, suppressAgentIds });
      lastReplyAt.current = created.created_at;
      return created.id;
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.queue.toast.failed));
      return false;
    }
  };

  // What a posted reply finishes depends on what was asked.
  const onReplyAccepted = () => {
    if (item.kind === "in_review" && requestingChanges) {
      moveStatus("in_progress", t(($) => $.queue.toast.changes_requested, { identifier }));
      return;
    }
    toast.success(t(($) => $.queue.toast.replied, { identifier }));
    // A Blocked issue stays blocked until the agent picks the reply up; keep
    // the entry, marked, rather than asking for the same reply again.
    if (item.kind === "blocked" && lastReplyAt.current) markReplied(item.key, lastReplyAt.current);
    archiveRelated();
    onDone();
  };

  const showComposer =
    !!issueId && (item.kind === "blocked" || item.kind === "mentioned" || requestingChanges);

  const cardLabel =
    item.kind === "in_review"
      ? t(($) => $.queue.card.delivery, { name: name ?? identifier })
      : item.kind === "blocked"
        ? t(($) => $.queue.card.blocked, { name: name ?? identifier })
        : item.kind === "mentioned"
          ? t(($) => $.queue.card.mention, { name: name ?? identifier })
          : t(($) => $.queue.card.notice);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {leading && (
        <div className="flex h-12 shrink-0 items-center border-b px-4">{leading}</div>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-5">
          <div className="flex flex-col gap-1.5">
            <div className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
              <NeedsMeKindChip item={item} label={copy.kindLabel(item)} />
              {identifier && <span>{identifier}</span>}
              {issueHref && (
                <Button
                  variant="ghost"
                  size="xs"
                  className="ml-auto text-muted-foreground"
                  render={<AppLink href={issueHref} />}
                  nativeButton={false}
                >
                  <ExternalLink className="size-3.5" />
                  {t(($) => $.queue.open_issue)}
                </Button>
              )}
            </div>
            <h2 className="text-title font-semibold">{copy.sentence(item)}</h2>
            <p className="text-caption text-muted-foreground">{copy.reasons(item).join(" · ")}</p>
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex min-w-0 items-center gap-2 text-caption">
              {comment ? (
                <ActorAvatar actorType={comment.actor_type} actorId={comment.actor_id} size="xs" />
              ) : null}
              <span className="font-medium">{cardLabel}</span>
              {comment && <span className="text-muted-foreground">{timeAgo(comment.created_at)}</span>}
            </div>
            {commentLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-4/5" />
              </div>
            ) : comment?.content ? (
              <ReadonlyContent content={comment.content} attachments={comment.attachments} />
            ) : item.inbox?.body ? (
              <p className="whitespace-pre-wrap text-body">{item.inbox.body}</p>
            ) : (
              <p className="text-body text-muted-foreground">{t(($) => $.queue.card.none)}</p>
            )}
          </div>

          {replied && (
            <p className="text-caption text-muted-foreground">
              {name
                ? t(($) => $.needs_me.replied, { name })
                : t(($) => $.needs_me.replied_anonymous)}
            </p>
          )}

          {showComposer && issueId && (
            <div className="flex flex-col gap-2">
              <p className="text-caption text-muted-foreground">
                {requestingChanges
                  ? t(($) => $.queue.request_changes_hint)
                  : name
                    ? t(($) => $.queue.reply_to, { name })
                    : t(($) => $.queue.reply)}
              </p>
              <CommentInput key={issueId} issueId={issueId} onSubmit={submitReply} onAccepted={onReplyAccepted} />
            </div>
          )}
        </div>
      </div>

      {/* pr-16 keeps "next" clear of the floating chat button in the corner. */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-t py-3 pl-6 pr-16">
        {item.kind === "in_review" && issue && (
          <>
            <Button
              size="sm"
              onClick={() => moveStatus("done", t(($) => $.queue.toast.approved, { identifier }))}
            >
              {t(($) => $.queue.action.approve)}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setRequestingChanges((value) => !value)}>
              {requestingChanges
                ? t(($) => $.queue.action.cancel_request_changes)
                : t(($) => $.queue.action.request_changes)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => moveStatus("todo", t(($) => $.queue.toast.back_to_todo, { identifier }))}
            >
              {t(($) => $.queue.action.back_to_todo)}
            </Button>
          </>
        )}
        {item.kind === "blocked" && issue && (
          <>
            <AssigneePicker
              assigneeType={issue.assignee_type}
              assigneeId={issue.assignee_id}
              onUpdate={reassign}
              trigger={t(($) => $.queue.action.reassign)}
              triggerRender={<Button size="sm" variant="outline" />}
              align="start"
            />
            <Button
              size="sm"
              variant="ghost"
              onClick={() => moveStatus("cancelled", t(($) => $.queue.toast.cancelled, { identifier }))}
            >
              {t(($) => $.queue.action.cancel_issue)}
            </Button>
          </>
        )}
        {item.kind === "mentioned" && (
          <Button size="sm" variant="ghost" onClick={dismiss}>
            {t(($) => $.queue.action.dismiss)}
          </Button>
        )}
        {item.kind === "action_required" && (
          <>
            {issueHref && (
              <Button size="sm" render={<AppLink href={issueHref} />} nativeButton={false}>
                {t(($) => $.queue.action.open)}
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={dismiss}>
              {t(($) => $.queue.action.dismiss)}
            </Button>
          </>
        )}

        {next && (
          <button
            type="button"
            onClick={() => onSelectNext(next.key)}
            className="ml-auto flex min-w-0 max-w-[50%] items-center gap-2 rounded-md px-2 py-1 text-caption text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
          >
            <span className="shrink-0">{t(($) => $.queue.next)}</span>
            <NeedsMeKindChip item={next} label={copy.kindLabel(next)} />
            <span className="truncate">{next.title}</span>
            <ArrowRight className="size-3.5 shrink-0" />
          </button>
        )}
      </div>
    </div>
  );
}
