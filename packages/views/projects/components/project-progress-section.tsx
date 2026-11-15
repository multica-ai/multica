"use client";

import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, User } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type {
  Project,
  ProjectProgress,
  ProjectProgressAssignee,
  ProjectProgressForecast,
  ProjectProgressPoint,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { projectProgressOptions } from "@multica/core/projects/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { useLocale, useT } from "../../i18n";
import { getProjectProgress } from "./project-issue-metrics";

const DAY_MS = 24 * 60 * 60 * 1000;

// Calendar days ("YYYY-MM-DD") are compared and formatted in UTC, the same
// clock the server samples history on.
function dayValue(date: string): number {
  return Date.parse(`${date}T00:00:00Z`);
}

function daysBetween(from: string, to: string): number {
  return Math.round((dayValue(to) - dayValue(from)) / DAY_MS);
}

function useFormatDay() {
  const locale = useLocale();
  const format = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });
  return (date: string) => format.format(dayValue(date));
}

export function ProjectProgressSection({ project }: { project: Project }) {
  const wsId = useWorkspaceId();
  const { data: detail } = useQuery(projectProgressOptions(wsId, project.id));
  const progress = detail?.progress ?? getProjectProgress(project);

  if (progress.scope === 0) {
    return <ProjectProgressEmpty project={project} cancelled={progress.cancelled} />;
  }

  return (
    <div className="space-y-3">
      <ProgressSummary progress={progress} />
      {detail && detail.history.length >= 2 && (
        <ProgressTrend history={detail.history} forecast={detail.forecast} dueDate={project.due_date} />
      )}
      {detail && (
        <ForecastNote progress={progress} history={detail.history} forecast={detail.forecast} dueDate={project.due_date} />
      )}
      {detail && detail.by_assignee.length > 0 && <AssigneeBreakdown rows={detail.by_assignee} />}
    </div>
  );
}

function ProjectProgressEmpty({ project, cancelled }: { project: Project; cancelled: number }) {
  const { t } = useT("projects");
  return (
    <div className="space-y-2">
      <div className="flex items-baseline gap-2">
        <span className="text-title font-semibold tabular-nums">0%</span>
        <span className="text-caption text-muted-foreground">{t(($) => $.detail.no_issues_yet)}</span>
      </div>
      <div className="h-2 rounded-full bg-muted" />
      {cancelled > 0 && <CancelledNote count={cancelled} />}
      <p className="text-caption text-muted-foreground">{t(($) => $.detail.progress_panel.empty_hint)}</p>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => useModalStore.getState().open("create-issue", { project_id: project.id })}
        >
          <Plus aria-hidden="true" />
          {t(($) => $.detail.empty_issues_new_button)}
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => useModalStore.getState().open("project-add-issue", { projectId: project.id })}
        >
          {t(($) => $.detail.empty_issues_add_existing)}
        </Button>
      </div>
    </div>
  );
}

function CancelledNote({ count }: { count: number }) {
  const { t } = useT("projects");
  return (
    <p className="text-caption text-muted-foreground">
      {t(($) => $.detail.progress_panel.cancelled_note, { count })}
    </p>
  );
}

function ProgressSummary({ progress }: { progress: ProjectProgress }) {
  const { t } = useT("projects");
  const share = (n: number) => `${(n / progress.scope) * 100}%`;
  const legend = [
    { key: "completed", label: t(($) => $.detail.progress_panel.legend_completed), count: progress.completed, swatch: "bg-info" },
    { key: "started", label: t(($) => $.detail.progress_panel.legend_started), count: progress.started, swatch: "bg-warning" },
    { key: "unstarted", label: t(($) => $.detail.progress_panel.legend_unstarted), count: progress.unstarted, swatch: "bg-muted" },
  ];
  return (
    <div className="space-y-2">
      <div className="flex items-baseline gap-2">
        <span className="text-title font-semibold tabular-nums">{progress.percent}%</span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.detail.progress_panel.summary, { count: progress.scope, completed: progress.completed })}
        </span>
      </div>
      <div
        className="flex h-2 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={legend.map((item) => `${item.label} ${item.count}`).join(", ")}
      >
        <div className="h-full bg-info transition-all" style={{ width: share(progress.completed) }} />
        <div className="h-full bg-warning transition-all" style={{ width: share(progress.started) }} />
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1 text-caption text-muted-foreground">
        {legend.map((item) => (
          <span key={item.key} className="inline-flex items-center gap-1.5">
            <span className={cn("size-2 rounded-[2px]", item.swatch)} aria-hidden="true" />
            {item.label}
            <span className="tabular-nums text-foreground">{item.count}</span>
          </span>
        ))}
      </div>
      {progress.cancelled > 0 && <CancelledNote count={progress.cancelled} />}
    </div>
  );
}

const CHART_W = 280;
const CHART_H = 96;
const CHART_TOP = 6;

function ProgressTrend({
  history,
  forecast,
  dueDate,
}: {
  history: ProjectProgressPoint[];
  forecast: ProjectProgressForecast | null;
  dueDate: string | null;
}) {
  const { t } = useT("projects");
  const formatDay = useFormatDay();
  const first = history[0]!;
  const today = history[history.length - 1]!;
  const due = dueDate && dayValue(dueDate) >= dayValue(first.date) ? dueDate : null;
  const end = [today.date, due, forecast?.completion_date]
    .filter((d): d is string => !!d)
    .reduce((a, b) => (dayValue(b) > dayValue(a) ? b : a));
  const span = Math.max(daysBetween(first.date, end), 1);
  const max = Math.max(...history.map((p) => p.scope), 1);

  const x = (date: string) => (daysBetween(first.date, date) / span) * CHART_W;
  const y = (n: number) => CHART_H - (n / max) * (CHART_H - CHART_TOP);
  const line = (value: (p: ProjectProgressPoint) => number) =>
    history.map((p) => `${x(p.date).toFixed(1)},${y(value(p)).toFixed(1)}`).join(" ");
  const band = (upper: (p: ProjectProgressPoint) => number, lower: (p: ProjectProgressPoint) => number) =>
    `${line(upper)} ${[...history].reverse().map((p) => `${x(p.date).toFixed(1)},${y(lower(p)).toFixed(1)}`).join(" ")}`;

  return (
    <figure className="space-y-1" aria-label={t(($) => $.detail.progress_panel.chart_label)}>
      <svg
        viewBox={`0 0 ${CHART_W} ${CHART_H}`}
        preserveAspectRatio="none"
        className="h-24 w-full overflow-visible"
        aria-hidden="true"
      >
        <line x1={0} y1={CHART_H} x2={CHART_W} y2={CHART_H} stroke="var(--border)" vectorEffect="non-scaling-stroke" />
        <polygon points={band((p) => p.completed + p.started, (p) => p.completed)} fill="var(--warning)" fillOpacity={0.22} />
        <polygon points={band((p) => p.completed, () => 0)} fill="var(--info)" fillOpacity={0.22} />
        <polyline points={line((p) => p.scope)} fill="none" stroke="var(--muted-foreground)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
        <polyline points={line((p) => p.completed + p.started)} fill="none" stroke="var(--warning)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
        <polyline points={line((p) => p.completed)} fill="none" stroke="var(--info)" strokeWidth={2} vectorEffect="non-scaling-stroke" />
        {forecast && (
          <>
            <line
              x1={x(today.date)} y1={y(today.completed)} x2={x(forecast.completion_date)} y2={y(today.scope)}
              stroke="var(--info)" strokeWidth={1.5} strokeDasharray="3 3" vectorEffect="non-scaling-stroke"
            />
            <line
              x1={x(forecast.completion_date)} y1={CHART_TOP} x2={x(forecast.completion_date)} y2={CHART_H}
              stroke="var(--info)" strokeOpacity={0.5} strokeDasharray="2 2" vectorEffect="non-scaling-stroke"
            />
          </>
        )}
        {due && (
          <line
            x1={x(due)} y1={0} x2={x(due)} y2={CHART_H}
            stroke="var(--destructive)" strokeWidth={1.5} vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <div className="relative h-4 whitespace-nowrap text-micro text-muted-foreground">
        <span className="absolute left-0">{formatDay(first.date)}</span>
        {x(today.date) > CHART_W * 0.2 && (!due || Math.abs(x(due) - x(today.date)) > CHART_W * 0.2) && (
          <AxisLabel at={x(today.date)}>{t(($) => $.detail.progress_panel.chart_today)}</AxisLabel>
        )}
        {due && (
          <AxisLabel at={x(due)} className="text-destructive">{formatDay(due)}</AxisLabel>
        )}
      </div>
    </figure>
  );
}

// Centered under its x position, pinned to the right edge at the end of the axis.
function AxisLabel({ at, className, children }: { at: number; className?: string; children: ReactNode }) {
  if (at >= CHART_W * 0.85) {
    return <span className={cn("absolute right-0", className)}>{children}</span>;
  }
  return (
    <span className={cn("absolute -translate-x-1/2", className)} style={{ left: `${(at / CHART_W) * 100}%` }}>
      {children}
    </span>
  );
}

function ForecastNote({
  progress,
  history,
  forecast,
  dueDate,
}: {
  progress: ProjectProgress;
  history: ProjectProgressPoint[];
  forecast: ProjectProgressForecast | null;
  dueDate: string | null;
}) {
  const { t } = useT("projects");
  const formatDay = useFormatDay();
  if (progress.completed >= progress.scope) return null;
  if (!forecast) {
    if (history.length < 2) return null;
    return <p className="text-caption text-muted-foreground">{t(($) => $.detail.progress_panel.forecast_none)}</p>;
  }
  const date = formatDay(forecast.completion_date);
  let text: string;
  if (!dueDate) {
    text = t(($) => $.detail.progress_panel.forecast_no_due, { date });
  } else {
    const slack = daysBetween(forecast.completion_date, dueDate);
    if (slack > 0) text = t(($) => $.detail.progress_panel.forecast_ahead, { date, count: slack });
    else if (slack < 0) text = t(($) => $.detail.progress_panel.forecast_behind, { date, count: -slack });
    else text = t(($) => $.detail.progress_panel.forecast_on_due, { date });
  }
  return (
    <p className={cn("text-caption", dueDate && daysBetween(forecast.completion_date, dueDate) < 0 ? "text-destructive" : "text-muted-foreground")}>
      {text}
    </p>
  );
}

function AssigneeBreakdown({ rows }: { rows: ProjectProgressAssignee[] }) {
  const { t } = useT("projects");
  const { getActorName } = useActorName();
  return (
    <div className="space-y-2">
      <div className="text-caption font-medium text-muted-foreground">
        {t(($) => $.detail.progress_panel.by_assignee)}
      </div>
      <ul className="space-y-1.5">
        {rows.map((row) => {
          const assigned = row.assignee_type && row.assignee_id;
          const name = assigned
            ? getActorName(row.assignee_type!, row.assignee_id!)
            : t(($) => $.detail.progress_panel.unassigned);
          return (
            <li key={`${row.assignee_type ?? "none"}:${row.assignee_id ?? ""}`} className="flex items-center gap-2 text-caption">
              {assigned ? (
                <ActorAvatar actorType={row.assignee_type!} actorId={row.assignee_id!} size="sm" />
              ) : (
                <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                  <User className="size-3" aria-hidden="true" />
                </span>
              )}
              <span className="min-w-0 flex-1 truncate">
                {name}
                {row.assignee_type === "agent" && (
                  <span className="ml-1.5 rounded border px-1 text-micro text-muted-foreground">
                    {t(($) => $.detail.progress_panel.agent_tag)}
                  </span>
                )}
              </span>
              <span className="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-muted">
                <span
                  className="block h-full rounded-full bg-info"
                  style={{ width: `${row.scope > 0 ? (row.completed / row.scope) * 100 : 0}%` }}
                />
              </span>
              <span className="w-10 shrink-0 text-right tabular-nums text-muted-foreground">
                {row.completed}/{row.scope}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
