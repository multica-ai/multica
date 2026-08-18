"use client";

import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueListOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { StatusIcon } from "./status-icon";

/**
 * Compact, presentation-only representation of an issue —
 * `<StatusIcon> <identifier> <title>`, bordered, with the title truncating to
 * an ellipsis once the chip hits its width cap.
 *
 * The cap is `min(18rem, 100%)` — two limits doing two different jobs:
 *   - `18rem` bounds the chip against the *content*, so a long title can never
 *     grow the chip wide enough to dominate a line of prose. Without it a chip
 *     is an atomic inline box that wraps to the next line as a unit whenever it
 *     doesn't fit at the current position, leaving a ragged gap on the line
 *     above — the reference-dense-prose problem from #6732.
 *   - `100%` bounds it against the *container*, keeping the chip inside narrow
 *     parents such as a chat bubble.
 * `ProjectChip` carries the identical cap; the two must not drift.
 *
 * The cap lives here (single source of truth) — wrappers must NOT add their own
 * flex container around it, or a percentage cap gets dropped during the
 * wrapper's intrinsic sizing and the clickable box diverges from the truncated
 * chip.
 *
 * This is the single source of truth for the "issue-mention card" look.
 * It is intentionally **not** a link or button: callers wrap it in whatever
 * interactive shell they need (AppLink for markdown mentions, an <a> with
 * cmd-click support inside the editor's NodeView, a plain span next to a
 * dismiss button in chat's context anchor card, …).
 *
 * Size budget: must fit within a 14px line-box when used inline — hence
 * `py-0.5` + text-caption (see MentionView docstring for the math).
 */
export interface IssueChipProps {
  issueId: string;
  /** Shown when the issue can't be resolved (deleted, other workspace, …). */
  fallbackLabel?: string;
  /** Optional content override for callers that need the shared chip shell. */
  children?: ReactNode;
  /** Extra classes — callers layer interaction hints here
   *  (e.g. `hover:bg-accent cursor-pointer` for navigable variants). */
  className?: string;
}

const BASE_CLASS =
  "issue-mention inline-flex min-w-0 max-w-[min(18rem,100%)] items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-caption";

export function IssueChip({
  issueId,
  fallbackLabel,
  children,
  className,
}: IssueChipProps) {
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

  if (children !== undefined) {
    return <span className={cls}>{children}</span>;
  }

  if (!issue) {
    return (
      <span className={cls}>
        <span className="min-w-0 truncate font-medium text-muted-foreground">
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
      <span className="min-w-0 truncate text-foreground">{issue.title}</span>
    </span>
  );
}
