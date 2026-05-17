"use client";

import { useQuery } from "@tanstack/react-query";
// CEREBRO-PATCH(issue-chip-run-indicator): import issueKeys + api for active-run indicator (JEH-1253)
import { issueListOptions, issueDetailOptions, issueKeys } from "@multica/core/issues/queries";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { StatusIcon } from "./status-icon";
import { AgentRunPip, taskStatusToRunState, type AgentRunState } from "../../common/agent-run-pip"; // CEREBRO-PATCH(issue-chip-run-state-pip): active vs queued indicator (JEH-1332)

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

// CEREBRO-PATCH(issue-chip-mobile-wrap): inline + box-decoration-clone so pill wraps across lines on mobile (JEH-1593); supersedes single-line layout from JEH-1253
const BASE_CLASS =
  "issue-mention inline rounded-md border mx-0.5 px-2 py-0.5 text-xs box-decoration-clone";

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

  // CEREBRO-PATCH(issue-chip-run-indicator): active-run detection for run-status dot (JEH-1253)
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    enabled: !!issue,
  });
  const runState = tasks.reduce<AgentRunState | undefined>((state, task) => {
    if (task.status !== "queued" && task.status !== "dispatched" && task.status !== "running") {
      return state;
    }
    const next = taskStatusToRunState(task.status);
    return next === "active" ? "active" : state ?? next;
  }, undefined);

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
    // CEREBRO-PATCH(issue-chip-readable): wrap long titles + tooltip instead of single-line truncate (JEH-1113)
    // CEREBRO-PATCH(issue-chip-multilne): identifier-secondary + full-title + run-indicator layout (JEH-1253)
    <span className={cls} title={issue.title}>
      <span className="flex items-center gap-1 shrink-0">
        <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
        {runState && <AgentRunPip state={runState} />}
        <span className="font-medium text-muted-foreground text-[10px]">{issue.identifier}</span>
      </span>
      {/* CEREBRO-PATCH(issue-chip-mobile-wrap): removed whitespace-nowrap so title wraps inline (JEH-1593) */}
      <span className="text-foreground">{issue.title}</span>
    </span>
  );
}
