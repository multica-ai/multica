// Project progress bar component.
// Calculates done/in-progress/todo counts from a list of issues,
// renders a stacked progress bar and count summary.

import type { Issue } from "@/shared/types";

interface ProjectProgressProps {
  /** Issues already filtered to a specific project. */
  issues: Issue[];
  /** compact=true renders a minimal single-row bar used in list cards. */
  compact?: boolean;
}

export function ProjectProgress({ issues, compact = false }: ProjectProgressProps) {
  // Exclude cancelled issues from all calculations.
  const active = issues.filter((i) => i.status !== "cancelled");

  const done = active.filter((i) => i.status === "done").length;
  const inProgress = active.filter(
    (i) => i.status === "in_progress" || i.status === "in_review",
  ).length;
  const todo = active.filter(
    (i) => i.status === "backlog" || i.status === "todo" || i.status === "blocked",
  ).length;
  const total = done + inProgress + todo;

  if (compact) {
    // In compact mode return null when there are no active issues.
    if (total === 0) return null;

    const donePct = (done / total) * 100;

    return (
      <div className="space-y-0.5">
        {/* Single-segment progress bar: done vs remaining */}
        <div className="flex h-1 w-full overflow-hidden rounded-full bg-muted">
          <div
            className="h-full bg-emerald-500 transition-all"
            style={{ width: `${donePct}%` }}
          />
        </div>
        <p className="text-xs text-muted-foreground">
          {done}/{total} done
        </p>
      </div>
    );
  }

  // Full mode (detail panel).
  if (total === 0) {
    return (
      <p className="text-sm text-muted-foreground">No active issues</p>
    );
  }

  const donePct = (done / total) * 100;
  const inProgressPct = (inProgress / total) * 100;

  // Build summary text, omitting zero-count groups.
  const parts: string[] = [];
  if (done > 0) parts.push(`${done} done`);
  if (inProgress > 0) parts.push(`${inProgress} in progress`);
  if (todo > 0) parts.push(`${todo} todo`);

  return (
    <div className="space-y-1.5">
      {/* Stacked three-segment progress bar */}
      <div className="flex h-1.5 w-full overflow-hidden rounded-full">
        <div className="h-full bg-emerald-500" style={{ width: `${donePct}%` }} />
        <div className="h-full bg-amber-500" style={{ width: `${inProgressPct}%` }} />
        {/* Remaining segment fills the rest */}
        <div className="h-full flex-1 bg-muted" />
      </div>
      <p className="text-xs text-muted-foreground">{parts.join(" · ")}</p>
    </div>
  );
}
