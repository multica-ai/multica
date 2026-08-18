"use client";

import { issueStatusCategory } from "@multica/core/issues";
import { useStatusLabel } from "./../utils/status-label";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { childIssueProgressOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import { descriptionPreview } from "./description-preview";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { StatusIcon } from "./status-icon";

interface IssueHoverCardProps {
  issueId: string;
  children: ReactNode;
  /**
   * Identifier to name the issue with when the detail fetch fails (e.g.
   * "MUL-7"). The same label the chip degrades to, so a card that cannot load
   * still says which issue it is about.
   */
  fallbackLabel?: string;
  /**
   * Open delay override, in milliseconds. Exists for tests only — production
   * passes nothing, so the card uses Base UI's default of 600ms. Tests pass 0
   * so hover assertions don't need to wait out the real delay.
   */
  delay?: number;
}

/**
 * Detail for an issue mention, on hover.
 *
 * The inline chip shows status, identifier, and as much title as fits inside
 * its `min(18rem, 100%)` cap — so a long title arrives truncated. The card
 * carries what the chip cannot: the full untruncated title, priority, a
 * description snippet, the assignee, and sub-issue progress. Member and agent
 * mentions already preview this way via MentionHoverCard
 * (packages/ui/components/common/mention-hover-card.tsx); this is the issue
 * equivalent, and lives here rather than in packages/ui because it reads
 * workspace queries.
 *
 * Every query lives in IssueHoverCardBody rather than here on purpose: Base UI
 * mounts the popup only while the card is open, so this component adds no
 * request of its own until a card opens. Moving a query up into IssueHoverCard
 * would fire one per mention on render. (The chip inside the trigger has its
 * own unresolved-issue fetch; that is independent of this.)
 */
export function IssueHoverCard({
  issueId,
  children,
  fallbackLabel,
  delay,
}: IssueHoverCardProps) {
  return (
    <HoverCard>
      {/* Rendered as a span, not the trigger's default anchor: the children are
          already an AppLink, and an anchor inside an anchor is invalid. */}
      <HoverCardTrigger delay={delay} render={<span />}>
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-auto min-w-56 max-w-80">
        <IssueHoverCardBody issueId={issueId} fallbackLabel={fallbackLabel} />
      </HoverCardContent>
    </HoverCard>
  );
}

/**
 * Assignee row of the card.
 *
 * Its own component so `useActorName` — which subscribes to the workspace
 * member list, a query nothing else on this path warms — mounts only for cards
 * that actually have an assignee to name. Inlining it back into the body would
 * make the first hover on any mention pull the member directory.
 */
function IssueHoverCardAssignee({
  actorType,
  actorId,
}: {
  actorType: string;
  actorId: string;
}) {
  const { getActorName } = useActorName();
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      {/* This is already a hover card, and the agent live-peek variant would
          fire its own request — so no nested card, no link. */}
      <ActorAvatar
        actorType={actorType}
        actorId={actorId}
        size="sm"
        enableHoverCard={false}
        profileLink={false}
        className="shrink-0"
      />
      <span className="min-w-0 truncate text-caption text-foreground">
        {getActorName(actorType, actorId)}
      </span>
    </span>
  );
}

function IssueHoverCardBody({
  issueId,
  fallbackLabel,
}: {
  issueId: string;
  fallbackLabel?: string;
}) {
  const wsId = useWorkspaceId();
  const resolveStatusLabel = useStatusLabel(wsId);
  const detail = useQuery(issueDetailOptions(wsId, issueId));
  // One workspace-wide progress snapshot shared with the issues list and issue
  // detail, not a per-issue children fetch: opening a card reuses the cache.
  const { data: childProgress } = useQuery(childIssueProgressOptions(wsId));
  const { t } = useT("issues");

  // A skeleton rather than localized loading text: only the pending phase gets
  // it, so a settled query never animates forever.
  if (detail.isPending) {
    return (
      <div data-testid="issue-hover-card-skeleton" className="flex flex-col gap-1.5">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="mt-1.5 h-3 w-44" />
        <Skeleton className="mt-1.5 h-4 w-24" />
      </div>
    );
  }

  const issue = detail.data;

  // Terminal state for every way the detail can fail to arrive: an error after
  // retries, a deleted issue, or a mention the viewer cannot read. Named with
  // the identifier the way the chip degrades, so the card still says which
  // issue it is about.
  if (detail.isError || !issue) {
    return (
      <div className="flex flex-col gap-1.5">
        {fallbackLabel && (
          <span className="text-caption font-medium text-muted-foreground">
            {fallbackLabel}
          </span>
        )}
        <p className="text-caption text-muted-foreground">{t(($) => $.detail.not_found)}</p>
      </div>
    );
  }

  const preview = issue.description ? descriptionPreview(issue.description) : "";
  const assigneeType = issue.assignee_type;
  const assigneeId = issue.assignee_id;
  const hasAssignee = !!assigneeType && !!assigneeId;
  const progress = childProgress?.get(issue.id);
  const hasProgress = !!progress && progress.total > 0;
  // Board cards and list rows render the glyph into a fixed grid slot, so the
  // "none" dash holds a column there. This card has no column to hold, leaving
  // the dash a mark carrying no information in an already dense popup.
  const hasPriority = !!issue.priority && issue.priority !== "none";

  return (
    <div className="flex flex-col gap-1.5">
      {/* Priority, status, identifier — the same lead-with-priority order as
          BoardCardContent, at the icon size the status glyph already uses.
          StatusIcon and PriorityIcon both render a bare <svg> that forwards no
          aria props, so each glyph is named by an `role="img"` wrapper: on a
          role-less span an aria-label is dropped by assistive tech. */}
      <div className="flex items-center gap-1.5">
        {hasPriority && (
          <span role="img" aria-label={t(($) => $.priority[issue.priority])} className="flex">
            <PriorityIcon priority={issue.priority} />
          </span>
        )}
        <span
          role="img"
          aria-label={resolveStatusLabel(issue.status)}
          className="flex shrink-0"
        >
          <StatusIcon
            status={issue.status}
            category={issueStatusCategory(issue) ?? undefined}
            className="h-3.5 w-3.5 shrink-0"
          />
        </span>
        <span className="text-caption font-medium text-muted-foreground">
          {issue.identifier}
        </span>
      </div>
      {/* The full title is the point of the card, so it never truncates — but
          an unbroken token (a pasted URL) would blow past the max-w-80 cap
          without a break opportunity. */}
      <p className="text-body text-foreground break-words">{issue.title}</p>

      {preview && (
        <p className="mt-1 text-caption text-muted-foreground line-clamp-2">{preview}</p>
      )}

      {(hasAssignee || hasProgress) && (
        <div className="mt-1 flex items-center justify-between gap-3">
          {hasAssignee ? (
            <IssueHoverCardAssignee actorType={assigneeType} actorId={assigneeId} />
          ) : (
            <span />
          )}
          {hasProgress && (
            <span className="inline-flex shrink-0 items-center gap-1">
              <ProgressRing done={progress.done} total={progress.total} size={12} />
              <span className="text-caption font-medium tabular-nums text-muted-foreground">
                {progress.done}/{progress.total}
              </span>
            </span>
          )}
        </div>
      )}
    </div>
  );
}
