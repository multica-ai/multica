"use client";

import { Fragment, useMemo, useState } from "react";
import { AlertTriangle, ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { ProjectPlanOverview, ProjectPlanPart } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../../navigation";
import { ActorAvatar } from "../../../common/actor-avatar";
import { useT } from "../../../i18n";
import { CoverageBadge, PlanProgressBar, SupersededBanner } from "./plan-shared";

const RISK_CHIP_LIMIT = 3;

/**
 * One matrix row. Drill-down lives on the
 * trailing chevron cell, linking to the part's first non-deleted issue — a
 * `<tr>` cannot itself be an anchor without breaking table semantics, so the
 * whole-row-clickable affordance the mockup implies is approximated this way.
 */
function PartRow({ part }: { part: ProjectPlanPart }) {
  const { t } = useT("issues");
  const p = useWorkspacePaths();
  const isGap = part.coverage_state === "no_tasks_yet" || part.coverage_state === "covered_no_active_tasks";
  const assignees = part.issues.filter((i) => !i.deleted && i.assignee_type && i.assignee_id);
  const firstLiveIssue = part.issues.find((i) => i.id && !i.deleted);

  return (
    <tr
      className={cn(
        "border-b border-border hover:bg-surface-hover transition-colors",
        isGap && "bg-warning/5 border-l-2 border-l-warning/50",
      )}
    >
      <td className="py-2 px-3">
        <span className={cn("text-body", isGap ? "text-muted-foreground" : "font-medium")}>{part.title}</span>
      </td>
      <td className="py-2 px-3">
        <CoverageBadge state={part.coverage_state} />
      </td>
      <td className="py-2 px-3 w-40">
        {isGap ? (
          <span className="text-caption text-muted-foreground">{t(($) => $.plan.coverage.not_decomposed)}</span>
        ) : (
          <PlanProgressBar done={part.rollup.tasks_done} total={part.rollup.tasks_total} />
        )}
      </td>
      <td className="py-2 px-3 w-24">
        {assignees.length > 0 ? (
          <div className="flex -space-x-1.5">
            {assignees.slice(0, 3).map((issue) => (
              <ActorAvatar
                key={issue.identifier}
                actorType={issue.assignee_type!}
                actorId={issue.assignee_id!}
                size="sm"
                className="ring-2 ring-surface"
              />
            ))}
          </div>
        ) : (
          <span className="text-caption text-muted-foreground">{t(($) => $.plan.coverage.no_assignees)}</span>
        )}
      </td>
      <td className="py-2 px-3 w-8 text-right text-faint-foreground">
        {firstLiveIssue?.id ? (
          <AppLink href={p.issueDetail(firstLiveIssue.id)} className="inline-flex">
            <ChevronRight className="size-3.5" />
          </AppLink>
        ) : (
          <ChevronRight className="size-3.5 inline opacity-40" />
        )}
      </td>
    </tr>
  );
}

/**
 * Concept C — coverage rollup as a scannable matrix, with a risk banner that
 * surfaces every part with no linked issues yet (the blind spot this view
 * exists to close).
 */
export function PlanCoveragePane({ overview }: { overview: ProjectPlanOverview }) {
  const { t } = useT("issues");
  const { plan, rollup, phases } = overview;
  const [expanded, setExpanded] = useState(false);

  const gapParts = useMemo(
    () =>
      phases.flatMap((phase) =>
        phase.parts
          .filter((part) => part.coverage_state === "no_tasks_yet")
          .map((part) => ({ part, phaseTitle: phase.title })),
      ),
    [phases],
  );
  const visibleGapChips = expanded ? gapParts : gapParts.slice(0, RISK_CHIP_LIMIT);
  const hiddenGapCount = gapParts.length - visibleGapChips.length;

  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <div className="px-6 py-5 max-w-5xl mx-auto">
        {plan.superseded && <SupersededBanner supersededAt={plan.superseded_at} />}

        <div className="grid grid-cols-4 gap-3 mb-4">
          <StatTile n={`${rollup.percent}%`} l={t(($) => $.plan.coverage.tile_percent)} />
          <StatTile n={`${rollup.tasks_done}/${rollup.tasks_total}`} l={t(($) => $.plan.coverage.tile_tasks)} />
          <StatTile n={`${rollup.parts_covered}/${rollup.parts_total}`} l={t(($) => $.plan.coverage.tile_parts_covered)} />
          <StatTile n={String(rollup.parts_without_tasks)} l={t(($) => $.plan.coverage.tile_gaps)} risk={rollup.parts_without_tasks > 0} />
        </div>

        {gapParts.length > 0 && (
          <div className="flex items-center gap-3 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 mb-4">
            <AlertTriangle className="size-5 shrink-0 text-warning" />
            <div className="min-w-0">
              <p className="text-body font-medium">
                {t(($) => $.plan.coverage.risk_banner_title, {
                  count: gapParts.length,
                  total: rollup.parts_total,
                })}
              </p>
              <p className="text-caption text-muted-foreground">{t(($) => $.plan.coverage.risk_banner_hint)}</p>
            </div>
            <div className="ml-auto flex flex-wrap gap-1.5 justify-end max-w-[46%]">
              {visibleGapChips.map(({ part }) => (
                <span key={part.id} className="inline-flex items-center gap-1 rounded-full bg-muted/60 px-2 py-0.5 text-micro text-muted-foreground">
                  <span className="size-1.5 rounded-full bg-warning" />
                  {part.title}
                </span>
              ))}
              {hiddenGapCount > 0 && (
                <button
                  type="button"
                  className="rounded-full bg-muted/60 px-2 py-0.5 text-micro text-muted-foreground hover:bg-muted"
                  onClick={() => setExpanded(true)}
                >
                  {t(($) => $.plan.coverage.risk_chip_more, { count: hiddenGapCount })}
                </button>
              )}
            </div>
          </div>
        )}

        <table className="w-full border border-surface-border rounded-lg overflow-hidden bg-surface">
          <thead>
            <tr className="bg-muted">
              <th className="text-left text-caption font-medium text-muted-foreground py-2 px-3">{t(($) => $.plan.coverage.col_part)}</th>
              <th className="text-left text-caption font-medium text-muted-foreground py-2 px-3">{t(($) => $.plan.coverage.col_coverage)}</th>
              <th className="text-left text-caption font-medium text-muted-foreground py-2 px-3 w-40">{t(($) => $.plan.coverage.col_progress)}</th>
              <th className="text-left text-caption font-medium text-muted-foreground py-2 px-3 w-24">{t(($) => $.plan.coverage.col_assignees)}</th>
              <th className="w-8" />
            </tr>
          </thead>
          <tbody>
            {phases.map((phase) => (
              <Fragment key={phase.id}>
                <tr className="bg-muted">
                  <td colSpan={5} className="py-2 px-3 text-label font-semibold">
                    {phase.title}
                    <span className="float-right text-caption font-medium text-muted-foreground">
                      {t(($) => $.plan.phase_rollup, {
                        done: phase.rollup.tasks_done,
                        total: phase.rollup.tasks_total,
                        percent: phase.rollup.percent,
                      })}
                    </span>
                  </td>
                </tr>
                {phase.parts.map((part) => (
                  <PartRow key={part.id} part={part} />
                ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatTile({ n, l, risk }: { n: string; l: string; risk?: boolean }) {
  return (
    <div
      className={cn(
        "rounded-lg border p-3.5",
        risk ? "border-warning/40 bg-warning/10" : "border-surface-border bg-surface",
      )}
    >
      <div className={cn("text-display-sm font-bold tracking-tight", risk && "text-warning")}>{n}</div>
      <div className="mt-0.5 text-caption text-muted-foreground">{l}</div>
    </div>
  );
}
