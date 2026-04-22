"use client";

import { useQuery } from "@tanstack/react-query";
import { issueListOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { StatusIcon } from "./status-icon";

/**
 * Compact, presentation-only representation of an issue —
 * `<StatusIcon> <identifier> <title>`, bordered, truncating to max-w-72.
 *
 * This is the single source of truth for the "issue-mention card" look.
 * It is intentionally **not** a link or button: callers wrap it in whatever
 * interactive shell they need (AppLink for markdown mentions, an <a> with
 * cmd-click support inside the editor's NodeView, a plain span next to a
 * dismiss button in chat's context anchor card, …).
 *
 * Size budget: must fit within a 14px line-box when used inline — hence
 * `py-0.5` + text-xs (see MentionView docstring for the math).
 */
export interface IssueChipProps {
  issueId: string;
  /** Shown when the issue can't be resolved (deleted, other workspace, …). */
  fallbackLabel?: string;
  /** Extra classes — callers layer interaction hints here
   *  (e.g. `hover:bg-accent cursor-pointer` for navigable variants). */
  className?: string;
}

const BASE_CLASS =
  "issue-mention inline-flex items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs max-w-72";

export function IssueChip({ issueId, fallbackLabel, className }: IssueChipProps) {
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const listIssue = issues.find((i) => i.id === issueId);

  // Fallback fetch for issues outside the first page of the list (e.g. Done).
  const { data: detailIssue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !listIssue,
  });

  const issue = listIssue ?? detailIssue;
  const cls = className ? `${BASE_CLASS} ${className}` : BASE_CLASS;

  if (!issue) {
    return (
      <span className={cls}>
        <span className="font-medium text-muted-foreground">
          {fallbackLabel ?? issueId.slice(0, 8)}
        </span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium text-muted-foreground shrink-0">
        {issue.identifier}
      </span>
      <span className="text-foreground truncate">{issue.title}</span>
    </span>
  );
}
