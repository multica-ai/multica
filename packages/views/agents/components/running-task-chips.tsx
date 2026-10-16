"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues";
import { projectListOptions } from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgentTask } from "@multica/core/types";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useLocale } from "../../i18n";
import { AppLink } from "../../navigation/app-link";

/** Locales whose typography expects a full-width separator between a prefix and
 *  a value (e.g. "项目：SHAR-123 标题"). */
function truncationSeparator(locale: string): string {
  return locale === "zh-Hans" || locale === "ja" || locale === "ko" ? "：" : ": ";
}

const CHIP_CLASS =
  "inline-flex shrink-0 items-center rounded-xs border border-surface-border bg-surface-hover px-1.5 py-0.5 font-mono text-micro font-medium text-muted-foreground transition-colors";

/**
 * Compact clickable chips showing an agent's currently running tasks.
 *
 * Each chip prefers the issue's human-readable identifier (e.g. `SHAR-123`,
 * resolved from `task.issue_id`); chat / autopilot tasks with no linked issue
 * fall back to the task's internal short id. A tooltip on hover shows the
 * issue identifier plus title, prefixed with the project name when the issue
 * belongs to a project (`<project>：SHAR-123 <title>`).
 *
 * Clicking a chip navigates to the issue; clicking inside a row/card that
 * itself navigates elsewhere must stopPropagation to avoid double navigation.
 */
export function RunningTaskChips({ tasks }: { tasks: AgentTask[] }) {
  if (tasks.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {tasks.map((task) => (
        <RunningTaskChip key={task.id} task={task} />
      ))}
    </div>
  );
}

function RunningTaskChip({ task }: { task: AgentTask }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const locale = useLocale();
  const [open, setOpen] = useState(false);

  const hasIssue = task.issue_id.length > 0;
  // Resolve the issue so the chip label can show the human-readable identifier
  // (SHAR-123) and the tooltip can show identifier + title. Cached + deduped by
  // TanStack; only fetched for issue-backed tasks.
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, task.issue_id),
    enabled: hasIssue,
  });

  const projectId = issue?.project_id ?? "";
  // Project name for the tooltip. Gated on hover so the agents page pays no
  // extra request until a chip is actually inspected; every chip shares the
  // same query key, so one request serves the whole workspace.
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: open && !!projectId,
  });
  const projectTitle = projectId
    ? (projects.find((p) => p.id === projectId)?.title ?? null)
    : null;

  // The internal short id is the fallback for tasks with no issue number (chat
  // sessions, autopilot runs, direct work without an issue).
  const shortId = task.id.slice(-4);
  const label = issue?.identifier ?? shortId;
  const href = hasIssue ? paths.issueDetail(task.issue_id) : null;

  let tooltip: string;
  if (issue) {
    const title = issue.title?.trim() ?? "";
    const issueLabel = title ? `${issue.identifier} ${title}` : issue.identifier;
    tooltip = projectTitle
      ? `${projectTitle}${truncationSeparator(locale)}${issueLabel}`
      : issueLabel;
  } else {
    tooltip = task.trigger_summary?.trim() || label;
  }

  const chip = href ? (
    <AppLink
      href={href}
      onClick={(e) => e.stopPropagation()}
      className={`${CHIP_CLASS} hover:border-brand/40 hover:bg-brand/5 hover:text-brand`}
    >
      {label}
    </AppLink>
  ) : (
    <span className={CHIP_CLASS}>{label}</span>
  );

  return (
    <Tooltip onOpenChange={setOpen}>
      <TooltipTrigger render={chip} />
      <TooltipContent side="bottom" className="max-w-xs">
        <p className="text-caption">{tooltip}</p>
      </TooltipContent>
    </Tooltip>
  );
}
