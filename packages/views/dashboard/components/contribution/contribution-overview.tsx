"use client";

import { useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { ContributionActorStats, ContributionSummary } from "@multica/core/types";
import { KpiCard } from "../../../runtimes/components/shared";
import { useLocale, useT } from "../../../i18n";
import { Segmented, type Dim } from "../dashboard-shared";
import { DimSegmented } from "../dim-segmented";
import type { ContributionScope } from "@multica/core/dashboard";
import { ContributionIssues } from "./contribution-issues";
import { ActorLabel, Card, LegendDot } from "./contribution-shared";
import {
  ACTOR_COLOR,
  buildHeatmap,
  splitActors,
  type ActorKind,
} from "./contribution-utils";

export function formatShortDate(iso: string, locale: string): string {
  return new Date(`${iso}T00:00:00`).toLocaleDateString(locale, { month: "2-digit", day: "2-digit" });
}

export function ContributionKpis({ summary }: { summary: ContributionSummary }) {
  const { t } = useT("usage");
  const k = summary.kpis;
  return (
    <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-5">
      <KpiCard label={t(($) => $.contribution.kpi.total)} value={k.total} hint={t(($) => $.contribution.kpi.total_hint)} />
      <KpiCard label={t(($) => $.contribution.kpi.active)} value={k.active} hint={t(($) => $.contribution.kpi.active_hint)} />
      <KpiCard
        label={t(($) => $.contribution.kpi.created)}
        value={k.created}
        hint={t(($) => $.contribution.kpi.created_hint, { member: k.created_by_member, agent: k.created_by_agent })}
      />
      <KpiCard label={t(($) => $.contribution.kpi.completed)} value={k.completed} hint={t(($) => $.contribution.kpi.completed_hint)} />
      <KpiCard
        label={t(($) => $.contribution.kpi.open)}
        value={k.open}
        hint={t(($) => $.contribution.kpi.open_hint, { progress: k.open_in_progress, review: k.open_in_review })}
      />
    </div>
  );
}

function TeamTrendCard({ summary, allowedDims }: { summary: ContributionSummary; allowedDims: readonly Dim[] }) {
  const { t } = useT("usage");
  const locale = useLocale();
  const [dim, setDim] = useState<Dim>("weekly");
  const effectiveDim: Dim = allowedDims.includes(dim) ? dim : allowedDims[0]!;
  const config = {
    active: { label: t(($) => $.contribution.trend.active), color: "var(--chart-1)" },
    collaborated: { label: t(($) => $.contribution.trend.collaborated), color: "var(--chart-3)" },
    delivered: { label: t(($) => $.contribution.trend.delivered), color: "var(--warning)" },
  } satisfies ChartConfig;
  const data = (effectiveDim === "weekly" ? summary.weekly : summary.daily).map((b) => ({
    ...b,
    label: formatShortDate(b.date, locale),
  }));
  return (
    <Card
      title={t(($) => $.contribution.trend.title)}
      aside={
        <>
          <LegendDot color={config.active.color} label={config.active.label} />
          <LegendDot color={config.collaborated.color} label={config.collaborated.label} />
          <LegendDot color={config.delivered.color} label={config.delivered.label} />
          <DimSegmented allowedDims={allowedDims} value={effectiveDim} onChange={setDim} />
        </>
      }
    >
      <ChartContainer config={config} className="aspect-[5/2] w-full">
        <LineChart data={data} margin={{ left: 0, right: 8, top: 4, bottom: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
          <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width="auto" />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Line dataKey="active" stroke="var(--color-active)" strokeWidth={2} dot={false} />
          <Line dataKey="collaborated" stroke="var(--color-collaborated)" strokeWidth={2} dot={false} />
          <Line dataKey="delivered" stroke="var(--color-delivered)" strokeWidth={2} dot={false} />
        </LineChart>
      </ChartContainer>
    </Card>
  );
}

function heatColor(level: number, inRange: boolean): string {
  if (!inRange) return "transparent";
  if (level === 0) return "var(--color-muted)";
  const opacities = ["25%", "50%", "75%", "100%"];
  return `color-mix(in oklch, var(--color-chart-1) ${opacities[level - 1]}, transparent)`;
}

function HeatmapCard({ summary, tz }: { summary: ContributionSummary; tz: string }) {
  const { t } = useT("usage");
  const locale = useLocale();
  const columns = useMemo(() => buildHeatmap(summary.daily, tz), [summary.daily, tz]);
  const weekdayLabels = useMemo(() => {
    const fmt = new Intl.DateTimeFormat(locale, { weekday: "narrow", timeZone: "UTC" });
    // 2026-01-05 is a Monday.
    return Array.from({ length: 7 }, (_, i) => fmt.format(new Date(Date.UTC(2026, 0, 5 + i))));
  }, [locale]);
  return (
    <Card
      title={t(($) => $.contribution.heatmap.title)}
      aside={<span className="text-caption text-muted-foreground">{t(($) => $.contribution.heatmap.caption)}</span>}
    >
      <div className="flex gap-2">
        <div className="grid grid-rows-7 gap-[3px] text-micro text-muted-foreground">
          {weekdayLabels.map((label, i) => (
            <span key={i} className="leading-none">{i % 2 === 0 ? label : ""}</span>
          ))}
        </div>
        <div className="grid flex-1 grid-flow-col grid-rows-7 gap-[3px]" style={{ gridTemplateColumns: `repeat(${columns.length}, minmax(0, 1fr))` }}>
          {columns.flatMap((col) =>
            col.map((cell) => (
              <span
                key={cell.date}
                title={cell.inRange ? t(($) => $.contribution.heatmap.cell, { date: cell.date, count: cell.count }) : undefined}
                className="aspect-square rounded-[2px]"
                style={{ background: heatColor(cell.level, cell.inRange) }}
              />
            )),
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-end gap-1 text-caption text-muted-foreground">
        {t(($) => $.contribution.heatmap.less)}
        {[0, 1, 2, 3, 4].map((l) => (
          <span key={l} aria-hidden className="size-2.5 rounded-[2px]" style={{ background: heatColor(l, true) }} />
        ))}
        {t(($) => $.contribution.heatmap.more)}
      </div>
    </Card>
  );
}

const BOARD_ROWS = 5;

function BoardColumn({
  title,
  rows,
  value,
  color,
  footnote,
}: {
  title: string;
  rows: ContributionActorStats[];
  value: (a: ContributionActorStats) => number;
  color: string;
  footnote?: string;
}) {
  const { t } = useT("usage");
  const ranked = [...rows].filter((r) => value(r) > 0).sort((a, b) => value(b) - value(a)).slice(0, BOARD_ROWS);
  const max = ranked.length > 0 ? value(ranked[0]!) : 0;
  return (
    <div className="min-w-0 pb-2">
      <div className="flex items-baseline justify-between px-4 pt-3 pb-1">
        <span className="text-body font-medium">{title}</span>
        <span className="text-caption text-muted-foreground">{t(($) => $.contribution.board.unit)}</span>
      </div>
      {ranked.length === 0 ? (
        <p className="px-4 py-4 text-caption text-muted-foreground">{t(($) => $.contribution.board.empty)}</p>
      ) : (
        <ol className="divide-y">
          {ranked.map((r, i) => (
            <li key={r.actor_id} className="grid grid-cols-[16px_minmax(0,1.3fr)_minmax(0,1fr)_36px] items-center gap-2.5 px-4 py-2">
              <span className="text-caption tabular-nums text-muted-foreground">{i + 1}</span>
              <ActorLabel actorType={r.actor_type} actorId={r.actor_id} />
              <span aria-hidden className="h-2 overflow-hidden rounded-full bg-muted">
                <span className="block h-full rounded-full" style={{ width: `${max ? (value(r) / max) * 100 : 0}%`, background: color }} />
              </span>
              <span className="text-right text-caption tabular-nums">{value(r)}</span>
            </li>
          ))}
        </ol>
      )}
      {footnote && <p className="px-4 pt-1 text-caption text-muted-foreground">{footnote}</p>}
    </div>
  );
}

function LeaderboardCard({ summary }: { summary: ContributionSummary }) {
  const { t } = useT("usage");
  const [kind, setKind] = useState<ActorKind>("member");
  const { members, agents } = useMemo(() => splitActors(summary.actors), [summary.actors]);
  const rows = kind === "member" ? members : agents;
  const color = ACTOR_COLOR[kind];
  return (
    <Card
      flush
      title={t(($) => $.contribution.board.title)}
      aside={
        <>
          <span className="text-caption text-muted-foreground">
            {kind === "member"
              ? t(($) => $.contribution.board.caption_people, { count: members.length })
              : t(($) => $.contribution.board.caption_agents, { count: agents.length })}
          </span>
          <Segmented
            label={t(($) => $.contribution.board.actor_type_label)}
            value={kind}
            onChange={setKind}
            options={[
              { label: t(($) => $.contribution.board.people), value: "member" },
              { label: t(($) => $.contribution.board.agents), value: "agent" },
            ]}
          />
        </>
      }
    >
      <div className="grid grid-cols-1 divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
        <BoardColumn title={t(($) => $.contribution.board.participation)} rows={rows} value={(r) => r.participated} color={color} />
        <BoardColumn title={t(($) => $.contribution.board.collaboration)} rows={rows} value={(r) => r.commented} color={color} />
        <BoardColumn
          title={t(($) => $.contribution.board.delivery)}
          rows={rows}
          value={(r) => r.completed}
          color={color}
          footnote={
            summary.system_completed > 0
              ? t(($) => $.contribution.board.system_completed, { count: summary.system_completed })
              : undefined
          }
        />
      </div>
    </Card>
  );
}

export function ContributionOverview({
  summary,
  allowedDims,
  scope,
}: {
  summary: ContributionSummary;
  allowedDims: readonly Dim[];
  scope: ContributionScope;
}) {
  return (
    <div className="space-y-5">
      <ContributionKpis summary={summary} />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <TeamTrendCard summary={summary} allowedDims={allowedDims} />
        <HeatmapCard summary={summary} tz={scope.tz} />
      </div>
      <LeaderboardCard summary={summary} />
      <ContributionIssues scope={scope} />
    </div>
  );
}
