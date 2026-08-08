"use client";

import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { StatusIcon } from "./status-icon";

interface IssueHoverCardProps {
  issueId: string;
  children: ReactNode;
}

/**
 * Reveals what the narrower issue-mention variants hide.
 *
 * `compact` and `plain` drop the issue title to keep prose readable; this card
 * brings it back on hover. `full` already shows the title and deliberately does
 * not use this.
 *
 * The detail query lives in IssueHoverCardBody rather than here on purpose:
 * Base UI mounts the popup only while the card is open, so a paragraph
 * containing many mentions issues no detail requests until one is hovered.
 * Moving the query up into IssueHoverCard would fire one request per mention on
 * render.
 */
export function IssueHoverCard({ issueId, children }: IssueHoverCardProps) {
  return (
    <HoverCard>
      <HoverCardTrigger delay={0} render={<span />}>
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

  // A skeleton rather than localized loading text — the card carries no copy of
  // its own, so it needs no translation keys.
  if (!issue) {
    return (
      <div className="flex flex-col gap-1.5">
        <div className="h-3 w-20 animate-pulse rounded bg-muted" />
        <div className="h-4 w-48 animate-pulse rounded bg-muted" />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
        <span className="text-caption font-medium text-muted-foreground">
          {issue.identifier}
        </span>
      </div>
      <p className="text-body text-foreground">{issue.title}</p>
    </div>
  );
}
