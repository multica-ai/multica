"use client";

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
import { ActorAvatar } from "../../common/actor-avatar";
import { descriptionPreview } from "./description-preview";
import { ProgressRing } from "./progress-ring";
import { StatusIcon } from "./status-icon";

interface IssueHoverCardProps {
  issueId: string;
  children: ReactNode;
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
 * carries what the chip cannot: the full untruncated title, a description
 * snippet, the assignee, and sub-issue progress. Member and agent mentions
 * already preview this way via MentionHoverCard
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
export function IssueHoverCard({ issueId, children, delay }: IssueHoverCardProps) {
  return (
    <HoverCard>
      {/* Rendered as a span, not the trigger's default anchor: the children are
          already an AppLink, and an anchor inside an anchor is invalid. */}
      <HoverCardTrigger delay={delay} render={<span />}>
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-auto min-w-56 max-w-80">
        <IssueHoverCardBody issueId={issueId} />
      </HoverCardContent>
    </HoverCard>
  );
}

function IssueHoverCardBody({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  // One workspace-wide progress snapshot shared with the issues list and issue
  // detail, not a per-issue children fetch: opening a card reuses the cache.
  const { data: childProgress } = useQuery(childIssueProgressOptions(wsId));
  const { getActorName } = useActorName();

  // A skeleton rather than localized loading text — the card carries no copy of
  // its own, so it needs no translation keys.
  if (!issue) {
    return (
      <div className="flex flex-col gap-1.5">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="mt-1.5 h-3 w-44" />
        <Skeleton className="mt-1.5 h-4 w-24" />
      </div>
    );
  }

  const preview = issue.description ? descriptionPreview(issue.description) : "";
  const assigneeType = issue.assignee_type;
  const assigneeId = issue.assignee_id;
  const hasAssignee = !!assigneeType && !!assigneeId;
  const progress = childProgress?.get(issue.id);
  const hasProgress = !!progress && progress.total > 0;

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
        <span className="text-caption font-medium text-muted-foreground">
          {issue.identifier}
        </span>
      </div>
      <p className="text-body text-foreground">{issue.title}</p>

      {preview && (
        <p className="mt-1 text-caption text-muted-foreground line-clamp-2">{preview}</p>
      )}

      {(hasAssignee || hasProgress) && (
        <div className="mt-1 flex items-center justify-between gap-3">
          {hasAssignee ? (
            <span className="flex min-w-0 items-center gap-1.5">
              {/* This is already a hover card, and the agent live-peek variant
                  would fire its own request — so no nested card, no link. */}
              <ActorAvatar
                actorType={assigneeType}
                actorId={assigneeId}
                size="sm"
                enableHoverCard={false}
                profileLink={false}
                className="shrink-0"
              />
              <span className="min-w-0 truncate text-caption text-foreground">
                {getActorName(assigneeType, assigneeId)}
              </span>
            </span>
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
