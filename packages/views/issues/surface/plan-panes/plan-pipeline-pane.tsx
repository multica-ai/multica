"use client";

import { useMemo } from "react";
import { CheckCircle2, ChevronRight, CircleDot, Lock } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { ProjectPlanDependency, ProjectPlanOverview, ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";
import { useT } from "../../../i18n";
import { CoverageBadge, PlanProgressBar, SupersededBanner } from "./plan-shared";

type LaneStatus = "complete" | "blocked" | "in_progress" | "not_started";

function laneStatus(phase: ProjectPlanPhase, blockedBy: ProjectPlanDependency[]): LaneStatus {
  if (phase.rollup.tasks_total > 0 && phase.rollup.tasks_done === phase.rollup.tasks_total) return "complete";
  if (blockedBy.length > 0) return "blocked";
  if (phase.rollup.tasks_done > 0 || phase.parts.some((p) => p.coverage_state === "in_progress")) return "in_progress";
  return "not_started";
}

const LANE_ICON: Record<LaneStatus, typeof CheckCircle2> = {
  complete: CheckCircle2,
  blocked: Lock,
  in_progress: CircleDot,
  not_started: Lock,
};

function PartCard({ part }: { part: ProjectPlanPart }) {
  const { t } = useT("issues");
  const isGap = part.coverage_state === "no_tasks_yet" || part.coverage_state === "covered_no_active_tasks";
  return (
    <div
      className={cn(
        "rounded-md border p-2.5",
        isGap
          ? "border-dashed border-warning/40 bg-warning/5"
          : "border-surface-border bg-surface shadow-[var(--surface-shadow)]",
      )}
    >
      <div className="flex items-start justify-between gap-1.5">
        <span className={cn("text-label font-medium leading-tight", isGap && "text-muted-foreground")}>
          {part.title}
        </span>
        <ChevronRight className="size-3 shrink-0 mt-0.5 text-faint-foreground" />
      </div>
      {isGap ? (
        <div className="mt-2">
          <CoverageBadge state={part.coverage_state} />
        </div>
      ) : (
        <>
          <PlanProgressBar done={part.rollup.tasks_done} total={part.rollup.tasks_total} className="mt-2" />
          <div className="mt-2 flex items-center justify-between">
            <span className="text-micro text-muted-foreground">
              {part.issues.filter((i) => !i.deleted).length}{" "}
              {t(($) => $.plan.pipeline.stat_tasks)}
            </span>
            <CoverageBadge state={part.coverage_state} />
          </div>
        </>
      )}
    </div>
  );
}

function Lane({ phase, blockedBy }: { phase: ProjectPlanPhase; blockedBy: ProjectPlanDependency[] }) {
  const { t } = useT("issues");
  const status = laneStatus(phase, blockedBy);
  const Icon = LANE_ICON[status];
  const coveredCount = phase.parts.filter((p) => p.coverage_state !== "no_tasks_yet").length;

  return (
    <div className="w-[260px] shrink-0 flex flex-col">
      <div
        className={cn(
          "pb-3 mb-3 border-b-2",
          status === "in_progress" ? "border-brand" : "border-border",
        )}
      >
        <div
          className={cn(
            "flex items-center gap-1.5 text-micro font-semibold uppercase tracking-wide mb-1",
            status === "in_progress" ? "text-brand" : "text-muted-foreground",
          )}
        >
          <Icon className="size-3" />
          {t(($) => $.plan.pipeline.lane_status[status])}
        </div>
        <div className="text-title-sm font-semibold mb-2">{phase.title}</div>
        <PlanProgressBar done={phase.rollup.tasks_done} total={phase.rollup.tasks_total} />
      </div>
      <div className="flex flex-col gap-2 flex-1">
        {phase.parts.map((part) => (
          <PartCard key={part.id} part={part} />
        ))}
      </div>
      <div
        className={cn(
          "mt-2.5 pt-2.5 border-t border-dashed border-border text-micro flex items-center gap-1.5",
          blockedBy.length > 0 ? "text-warning" : "text-muted-foreground",
        )}
      >
        {blockedBy.length > 0 ? (
          <Lock className="size-3 shrink-0" />
        ) : (
          <CheckCircle2 className="size-3 shrink-0" />
        )}
        <span>
          {blockedBy.length > 0
            ? blockedBy
                .map((dep) =>
                  dep.blocking.missing
                    ? t(($) => $.plan.pipeline.blocked_missing)
                    : dep.blocking.type === "phase"
                      ? t(($) => $.plan.pipeline.blocked_on_phase, { title: dep.blocking.title })
                      : t(($) => $.plan.pipeline.blocked_on_part, {
                          title: dep.blocking.title,
                          phase: dep.blocking.phase_title ?? "",
                        }),
                )
                .join(" · ")
            : t(($) => $.plan.pipeline.gate_covered, { covered: coveredCount, total: phase.parts.length })}
        </span>
      </div>
    </div>
  );
}

/**
 * Concept B — phases as pipeline lanes, dependency gating rendered from the
 * real `dependencies` edges the API exposes (not inferred phase order).
 */
export function PlanPipelinePane({ overview }: { overview: ProjectPlanOverview }) {
  const { t } = useT("issues");
  const { plan, rollup, phases, dependencies } = overview;

  const blockedByPhase = useMemo(() => {
    const map = new Map<string, ProjectPlanDependency[]>();
    for (const dep of dependencies) {
      if (dep.blocked.type !== "phase") continue;
      const list = map.get(dep.blocked.id) ?? [];
      list.push(dep);
      map.set(dep.blocked.id, list);
    }
    return map;
  }, [dependencies]);

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="flex items-center gap-4 px-5 py-3 border-b border-border flex-wrap">
        <span className="text-title font-semibold">{plan.title}</span>
        <div className="w-px h-5 bg-border" />
        <span className="text-caption text-muted-foreground">
          <b className="text-foreground font-semibold">{rollup.percent}%</b> {t(($) => $.plan.pipeline.stat_percent)}
        </span>
        <span className="text-caption text-muted-foreground">
          <b className="text-foreground font-semibold">
            {rollup.tasks_done}/{rollup.tasks_total}
          </b>{" "}
          {t(($) => $.plan.pipeline.stat_tasks)}
        </span>
        {rollup.parts_without_tasks > 0 && (
          <span className="text-caption text-warning">
            <b className="font-semibold">
              {rollup.parts_without_tasks}/{rollup.parts_total}
            </b>{" "}
            {t(($) => $.plan.pipeline.stat_gaps)}
          </span>
        )}
      </div>

      <div className="flex-1 min-h-0 overflow-auto">
        <div className="p-6">
          {plan.superseded && <SupersededBanner supersededAt={plan.superseded_at} />}
          <div className="flex items-start gap-5">
            {phases.map((phase, i) => (
              <div key={phase.id} className="flex items-start gap-5">
                {i > 0 && <ChevronRight className="size-5 mt-16 shrink-0 text-faint-foreground" />}
                <Lane phase={phase} blockedBy={blockedByPhase.get(phase.id) ?? []} />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
