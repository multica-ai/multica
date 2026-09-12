"use client";

import { AlertTriangle, ChevronRight, History } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { ProjectPlanCoverageState, ProjectPlanIssueDetail } from "@multica/core/types";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
import { ActorAvatar } from "../../../common/actor-avatar";
import { AppLink } from "../../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../../../i18n";

/**
 * The 5 coverage states a plan part can be in, each visually distinct.
 * `not_started` and `covered_no_active_tasks` both
 * read as neutral/gray at a glance but differ in border style and copy —
 * the former has live, undone work; the latter has none left to do.
 */
export function CoverageBadge({ state, className }: { state: ProjectPlanCoverageState; className?: string }) {
  const { t } = useT("issues");
  const label = t(($) => $.plan.coverage_state[state]);
  const toneClass: Record<ProjectPlanCoverageState, string> = {
    complete: "bg-success/15 text-success",
    in_progress: "bg-info/15 text-info",
    not_started: "bg-muted text-muted-foreground",
    no_tasks_yet:
      "bg-warning/15 text-warning border border-dashed border-warning/50",
    covered_no_active_tasks:
      "bg-muted/50 text-muted-foreground border border-dashed border-border",
  };
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-micro font-medium whitespace-nowrap",
        toneClass[state],
        className,
      )}
    >
      {state === "no_tasks_yet" && <AlertTriangle className="size-2.5" />}
      {label}
    </span>
  );
}

/** Shared progress track — green when complete, blue while in flight. */
export function PlanProgressBar({ done, total, className }: { done: number; total: number; className?: string }) {
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div className={cn("flex flex-1 items-center gap-2", className)}>
      <div className="relative h-1.5 flex-1 rounded-full bg-muted overflow-hidden">
        <div
          className={cn("absolute inset-y-0 left-0 rounded-full", pct >= 100 ? "bg-success" : "bg-info")}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="w-10 shrink-0 text-right text-micro text-muted-foreground tabular-nums">
        {done}/{total}
      </span>
    </div>
  );
}

const ISSUE_STATUS_TONE: Record<string, string> = {
  backlog: "text-muted-foreground",
  todo: "text-muted-foreground",
  in_progress: "text-warning",
  in_review: "text-success",
  done: "text-info",
  blocked: "text-destructive",
  cancelled: "text-muted-foreground line-through decoration-muted-foreground/60",
};

/**
 * One issue's live status label, colored by category — a superseded plan
 * version renders these from the same fresh read as the active plan, so the
 * status shown is never a frozen snapshot: a superseded plan shows live
 * issue status.
 */
export function IssueStatusLabel({ issue, catalog }: { issue: ProjectPlanIssueDetail; catalog: IssueStatusCatalog }) {
  const category = catalog.categoryOf(issue.status);
  const tone = ISSUE_STATUS_TONE[category] ?? "text-muted-foreground";
  return <span className={cn("text-micro font-medium", tone)}>{catalog.labelOf(issue.status)}</span>;
}

/**
 * Drill-down from a plan part to one of its issues. A deleted issue has no
 * id to link to, so it renders as
 * struck-through text instead of a broken link.
 */
export function PlanIssueChip({ issue, catalog }: { issue: ProjectPlanIssueDetail; catalog: IssueStatusCatalog }) {
  const p = useWorkspacePaths();
  const content = (
    <>
      <span className="w-16 shrink-0 text-micro text-muted-foreground tabular-nums">{issue.identifier}</span>
      <IssueStatusLabel issue={issue} catalog={catalog} />
      <span
        className={cn(
          "min-w-0 flex-1 truncate text-caption",
          issue.deleted ? "text-muted-foreground line-through" : "text-foreground",
        )}
      >
        {issue.title}
      </span>
      {issue.assignee_type && issue.assignee_id && (
        <ActorAvatar actorType={issue.assignee_type} actorId={issue.assignee_id} size="sm" className="shrink-0" />
      )}
    </>
  );
  const className = "group flex items-center gap-2 rounded-md px-2 py-1 hover:bg-surface-hover transition-colors";
  if (!issue.id || issue.deleted) {
    return <div className={cn(className, "cursor-default")}>{content}</div>;
  }
  return (
    <AppLink href={p.issueDetail(issue.id)} className={className}>
      {content}
      <ChevronRight className="size-3 shrink-0 text-faint-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
    </AppLink>
  );
}

/**
 * Shown when rendering a retained (non-active) plan version. The issue rows
 * below it are still a live read, not a frozen copy — this banner exists so
 * that is legible to the viewer, not to change what gets fetched.
 */
export function SupersededBanner({ supersededAt }: { supersededAt: string | null }) {
  const { t } = useT("issues");
  const date = supersededAt ? new Date(supersededAt).toLocaleDateString() : "";
  return (
    <div className="flex items-center gap-2 rounded-md border border-dashed border-border bg-muted/40 px-3 py-2 text-caption text-muted-foreground mb-4">
      <History className="size-3.5 shrink-0" />
      <span>{t(($) => $.plan.superseded_banner, { date })}</span>
    </div>
  );
}
