"use client";

import { useQuery } from "@tanstack/react-query";
import { issueListOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueMentionMode } from "@multica/core/issues/stores";
import { StatusIcon } from "./status-icon";

/**
 * Compact, presentation-only representation of an issue —
 * `<StatusIcon> <identifier> <title>`, bordered, capped at the container width
 * (`max-w-full`) with the title truncating to an ellipsis. As an atomic inline
 * box it wraps to the next line as a unit when it doesn't fit at the current
 * position; the ellipsis only kicks in once a whole line can't hold it. The cap
 * lives here (single source of truth) — wrappers must NOT add their own flex
 * container around it, or a percentage cap gets dropped during the wrapper's
 * intrinsic sizing and the clickable box diverges from the truncated chip.
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
  /**
   * How much of the issue to show. `full` (default) is the historical
   * rendering. `compact` keeps the box and status icon but drops the title;
   * `plain` drops the box entirely and reads as prose. Callers that render
   * content mentions pass the reader's preference; callers placing a
   * deliberate UI affordance (chat's context anchor card) leave it `full`.
   */
  variant?: IssueMentionMode;
  /** Extra classes — callers layer interaction hints here
   *  (e.g. `hover:bg-accent cursor-pointer` for navigable variants). */
  className?: string;
}

const BASE_CLASS =
  "issue-mention inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-caption";

// `plain` deliberately inherits the surrounding prose font size instead of
// `text-caption` — it should read as part of the sentence, not as a shrunken
// chip. Bare text is well inside the 22.75px line box the editor NodeView
// sizing math protects (see mention-view.tsx).
//
// It also deliberately omits `min-w-0 truncate`, which the boxed variants need.
// A bare identifier is short and should wrap with the sentence around it; an
// ellipsis mid-prose would read as damage. Do not "restore" truncation here.
//
// Uses `text-brand` (the link color token, see prose.css `.rich-text-editor a`)
// rather than `text-primary`: `--primary` is a solid FILL color for buttons
// and is near-white in dark mode, which made this render indistinguishable
// from surrounding prose. `--brand` is the token this codebase actually uses
// for link-colored text.
const PLAIN_CLASS = "issue-mention text-brand hover:underline";

export function IssueChip({ issueId, fallbackLabel, variant = "full", className }: IssueChipProps) {
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const listIssue = issues.find((i) => i.id === issueId);

  // Fallback fetch for issues outside the first page of the list (e.g. Done).
  const { data: detailIssue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !listIssue,
  });

  const issue = listIssue ?? detailIssue;
  const base = variant === "plain" ? PLAIN_CLASS : BASE_CLASS;
  const cls = className ? `${base} ${className}` : base;

  const label = issue?.identifier ?? fallbackLabel ?? issueId.slice(0, 8);

  if (variant === "plain") {
    return <span className={cls}>{label}</span>;
  }

  if (!issue) {
    return (
      <span className={cls}>
        <span className="min-w-0 truncate font-medium text-muted-foreground">{label}</span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium text-muted-foreground shrink-0">{issue.identifier}</span>
      {variant === "full" && (
        <span className="min-w-0 truncate text-foreground">{issue.title}</span>
      )}
    </span>
  );
}
