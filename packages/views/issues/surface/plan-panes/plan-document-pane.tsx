"use client";

import { FileWarning } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { ProjectPlanOverview, ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";
import { useIssueStatuses } from "@multica/core/issue-statuses";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../../../i18n";
import { CoverageBadge, PlanIssueChip, PlanProgressBar, SupersededBanner } from "./plan-shared";
import {
  AddPhaseButton,
  PartActions,
  PhaseActions,
  PlanHeaderActions,
} from "./authoring/plan-authoring-controls";

function PartBlock({ phase, part }: { phase: ProjectPlanPhase; part: ProjectPlanPart }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const isGapLike = part.coverage_state === "no_tasks_yet" || part.coverage_state === "covered_no_active_tasks";

  return (
    <div
      className={cn(
        "group/plan-part relative pl-4 mb-5 border-l-2",
        part.coverage_state === "no_tasks_yet" ? "border-dashed border-warning/50" : "border-border",
      )}
    >
      <div className="flex items-center justify-between gap-3 mb-2">
        <h3 className={cn("text-title-sm font-semibold", isGapLike && "text-muted-foreground")}>{part.title}</h3>
        <div className="flex items-center gap-2.5 min-w-0">
          {/* The badge renders for all 5 states, not just the 2 gap states —
              `in_progress` at 0 done and `not_started` at 0 done both show an
              identical 0/N progress bar, so the bar alone cannot distinguish
              them. The badge is what makes every state visually distinct.
              */}
          {!isGapLike && (
            <PlanProgressBar done={part.rollup.tasks_done} total={part.rollup.tasks_total} className="max-w-[140px]" />
          )}
          <CoverageBadge state={part.coverage_state} />
          <PartActions phase={phase} part={part} />
        </div>
      </div>

      {part.coverage_state === "no_tasks_yet" && (
        <div className="flex items-start gap-2.5 rounded-md border border-dashed border-warning/40 bg-warning/5 px-3 py-2.5">
          <FileWarning className="size-4 shrink-0 mt-0.5 text-warning" />
          <div>
            <p className="text-body font-medium">{t(($) => $.plan.coverage_hint.no_tasks_yet)}</p>
          </div>
        </div>
      )}

      {part.coverage_state === "covered_no_active_tasks" && (
        <div className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2.5 mb-1">
          <p className="text-caption text-muted-foreground">{t(($) => $.plan.coverage_hint.covered_no_active_tasks)}</p>
        </div>
      )}

      {part.issues.length > 0 && (
        <div className="flex flex-col gap-0.5">
          {part.issues.map((issue) => (
            <PlanIssueChip key={issue.identifier} issue={issue} catalog={catalog} />
          ))}
        </div>
      )}
    </div>
  );
}

function PhaseBlock({ phase }: { phase: ProjectPlanPhase }) {
  const { t } = useT("issues");
  const gapCount = phase.parts.filter(
    (p) => p.coverage_state === "no_tasks_yet" || p.coverage_state === "covered_no_active_tasks",
  ).length;
  return (
    <div className="group/plan-phase mb-7">
      <div className="flex items-baseline justify-between gap-3 pb-2 mb-3.5 border-b border-border">
        <h2 className="text-title-lg font-semibold tracking-tight">{phase.title}</h2>
        <span className="flex items-center gap-2 text-caption text-muted-foreground whitespace-nowrap">
          {t(($) => $.plan.phase_rollup, {
            done: phase.rollup.tasks_done,
            total: phase.rollup.tasks_total,
            percent: phase.rollup.percent,
          })}
          {gapCount > 0 && (
            <>
              {" · "}
              {t(($) => $.plan.part_gap_more, { count: gapCount })}
            </>
          )}
          <PhaseActions phase={phase} />
        </span>
      </div>
      {phase.parts.map((part) => (
        <PartBlock key={part.id} phase={phase} part={part} />
      ))}
    </div>
  );
}

/**
 * Concept A — a top-to-bottom read of the plan as a document: phases in
 * order, each part's progress or gap, drill-down into its issues. Wired to
 * the live read model — every number here is `overview`, nothing computed
 * or guessed client-side.
 */
export function PlanDocumentPane({ overview }: { overview: ProjectPlanOverview }) {
  const { t } = useT("issues");
  const { plan, rollup, phases } = overview;
  const donePct = rollup.tasks_total > 0 ? Math.round((rollup.tasks_done / rollup.tasks_total) * 100) : 0;
  const inProgressPct = Math.max(0, 100 - donePct);

  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <div className="max-w-3xl mx-auto px-10 py-9 pb-24">
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h1 className="text-display-sm font-semibold tracking-tight">{plan.title}</h1>
            <p className="mt-1 text-body text-muted-foreground">
              {t(($) => $.plan.document.subtitle, { version: plan.version, phaseCount: phases.length })}
            </p>
          </div>
          <PlanHeaderActions />
        </div>

        {plan.superseded && <SupersededBanner supersededAt={plan.superseded_at} />}

        <div className="flex items-center gap-5 p-3.5 mb-8 rounded-lg border border-surface-border bg-surface shadow-[var(--surface-shadow)]">
          <div className="flex flex-col gap-0.5">
            <span className="text-title-lg font-semibold">{rollup.percent}%</span>
            <span className="text-caption text-muted-foreground">{t(($) => $.plan.document.stat_percent)}</span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-title-lg font-semibold">
              {rollup.tasks_done}/{rollup.tasks_total}
            </span>
            <span className="text-caption text-muted-foreground">{t(($) => $.plan.document.stat_tasks)}</span>
          </div>
          {rollup.parts_without_tasks > 0 && (
            <div className="flex flex-col gap-0.5">
              <span className="text-title-lg font-semibold text-warning">
                {rollup.parts_without_tasks}/{rollup.parts_total}
              </span>
              <span className="text-caption text-muted-foreground">{t(($) => $.plan.document.stat_gaps)}</span>
            </div>
          )}
          <div className="flex-1 flex flex-col gap-1.5">
            <div className="flex h-2 rounded-full overflow-hidden bg-muted">
              <div className="bg-success" style={{ width: `${donePct}%` }} />
              <div className="bg-info/40" style={{ width: `${inProgressPct}%` }} />
            </div>
          </div>
        </div>

        {phases.map((phase) => (
          <PhaseBlock key={phase.id} phase={phase} />
        ))}

        <AddPhaseButton />
      </div>
    </div>
  );
}
