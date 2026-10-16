"use client";

import { useMemo } from "react";
import type { AgentTask } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { AppLink } from "../../navigation/app-link";

/**
 * Compact clickable chips showing an agent's currently running tasks.
 *
 * Each chip displays the issue identifier (short form) and links to the issue
 * detail page. A tooltip on hover shows the task's `trigger_summary` — the
 * canonical short description the server captured at creation time.
 *
 * Clicking a chip navigates to the issue; clicking inside a row/card that
 * itself navigates elsewhere must stopPropagation to avoid double navigation.
 */
export function RunningTaskChips({
  tasks,
}: {
  tasks: AgentTask[];
}) {
  const paths = useWorkspacePaths();

  // Pre-compute the short issue label (last 4 chars of the UUID is the
  // convention used elsewhere in the product for short issue refs).
  const chips = useMemo(
    () =>
      tasks.map((task) => {
        const issueId = task.issue_id;
        const shortId = issueId ? issueId.slice(-4) : "----";
        const tooltip = task.trigger_summary?.trim() || `Issue ${shortId}`;
        const href = issueId ? paths.issueDetail(issueId) : null;
        return { shortId, tooltip, href, key: task.id };
      }),
    [tasks, paths],
  );

  if (chips.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {chips.map((chip) => (
        <Tooltip key={chip.key}>
          <TooltipTrigger
            render={
              chip.href ? (
                <AppLink
                  href={chip.href}
                  onClick={(e) => e.stopPropagation()}
                  className="inline-flex shrink-0 items-center rounded-xs border border-surface-border bg-surface-hover px-1.5 py-0.5 font-mono text-micro font-medium text-muted-foreground transition-colors hover:border-brand/40 hover:bg-brand/5 hover:text-brand"
                >
                  {chip.shortId}
                </AppLink>
              ) : (
                <span className="inline-flex shrink-0 items-center rounded-xs border border-surface-border bg-surface-hover px-1.5 py-0.5 font-mono text-micro font-medium text-muted-foreground">
                  {chip.shortId}
                </span>
              )
            }
          />
          <TooltipContent side="bottom" className="max-w-xs">
            <p className="text-caption">{chip.tooltip}</p>
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}
