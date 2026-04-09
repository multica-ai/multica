"use client";

import { AppLink } from "../../navigation";
import { useQuery } from "@tanstack/react-query";
import { issueListOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { StatusIcon } from "./status-icon";

interface IssueMentionCardProps {
  issueId: string;
  /** Fallback text when issue is not in store (e.g. "MUL-7") */
  fallbackLabel?: string;
}

export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const issue = issues.find((i) => i.id === issueId);

  if (!issue) {
    return (
      <AppLink
        href={`/issues/${issueId}`}
        className="issue-mention inline-flex items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs hover:bg-accent transition-colors cursor-pointer max-w-72"
      >
        <span className="font-medium text-muted-foreground">
          {fallbackLabel ?? issueId.slice(0, 8)}
        </span>
      </AppLink>
    );
  }

  return (
    <AppLink
      href={`/issues/${issueId}`}
      className="issue-mention inline-flex items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs hover:bg-accent transition-colors cursor-pointer max-w-72"
    >
      <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium text-muted-foreground shrink-0">{issue.identifier}</span>
      <span className="text-foreground truncate">{issue.title}</span>
    </AppLink>
  );
}
