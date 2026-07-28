"use client";

import { useState, type Dispatch, type SetStateAction } from "react";
import type { AnalyticsDimension, AnalyticsOperator } from "@multica/cerebro-usage";
import type { DashboardOverview, TopActor } from "../../core/api";
import {
  DEFAULT_ANALYTICS_VISUALS,
  removeAnalyticsFilterValue,
  addAnalyticsFilterValue,
  type AnalyticsFilter,
} from "../../core/analytics";
import { useDashboardStore } from "../../core/store";
import { AnalyticsDashboard } from "./analytics-dashboard";
import {
  ControlRoomEmpty,
  ControlRoomKpi,
  ControlRoomLoading,
  ControlRoomPanel,
  MetricBar,
  formatCompact,
  formatDollars,
} from "./control-room-primitives";
import { RunsToolbar } from "./runs-toolbar";

export function OverviewControlRoom({
  workspaceId,
  data,
  isLoading,
  filters,
  onFiltersChange,
  onNewVisual,
  onSelectActor,
  builderOpen,
  onBuilderOpenChange,
}: {
  workspaceId: string;
  data: DashboardOverview | undefined;
  isLoading: boolean;
  filters: AnalyticsFilter[];
  onFiltersChange: Dispatch<SetStateAction<AnalyticsFilter[]>>;
  onNewVisual: () => void;
  onSelectActor: (actorId: string, actorName: string, actorType: "member" | "agent") => void;
  builderOpen?: boolean;
  onBuilderOpenChange?: (open: boolean) => void;
}) {
  const range = useDashboardStore((state) => state.range);
  const setRange = useDashboardStore((state) => state.setRange);
  const [compactLayout, setCompactLayout] = useState(false);

  const addFilter = (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in" | "contains" | "not_contains") => {
    onFiltersChange((current) => addAnalyticsFilterValue(current, dimension, value, operator));
  };
  const removeFilter = (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => {
    onFiltersChange((current) => removeAnalyticsFilterValue(current, dimension, value, operator));
  };
  const filterDay = (day: string) => {
    const start = new Date(`${day}T00:00:00.000Z`);
    const end = new Date(start);
    end.setUTCDate(end.getUTCDate() + 1);
    onFiltersChange((current) => [
      ...current.filter((filter) => filter.dimension !== "time"),
      { dimension: "time", operator: "gte", values: [start.toISOString()] },
      { dimension: "time", operator: "lte", values: [end.toISOString()] },
    ]);
  };

  const maxThroughput = Math.max(1, ...(data?.timeline ?? []).map((bucket) => bucket.tasks_completed + bucket.issues_done));

  return (
    <div className={`min-w-0 p-6 ${compactLayout ? "space-y-2" : "space-y-3"}`}>
      <RunsToolbar
        workspaceId={workspaceId}
        range={range}
        onRangeChange={setRange}
        filters={filters}
        onAddFilter={addFilter}
        onRemoveFilter={removeFilter}
        onClear={() => onFiltersChange([])}
        onCustomize={() => setCompactLayout((current) => !current)}
        onNewVisual={onNewVisual}
      />

      <section aria-label="Workspace pulse" className="grid grid-cols-2 divide-x divide-y overflow-hidden rounded-lg border bg-card sm:grid-cols-4 xl:grid-cols-7 xl:divide-y-0">
        <ControlRoomKpi label="Issues opened" value={formatCompact(data?.issues_created.value ?? 0)} note="in selected range" />
        <ControlRoomKpi label="Issues completed" value={formatCompact(data?.issues_completed.value ?? 0)} note="work moved to done" tone="positive" />
        <ControlRoomKpi label="Tasks completed" value={formatCompact(data?.tasks_completed.value ?? 0)} note="agent outcomes" tone="positive" />
        <ControlRoomKpi label="Tasks failed" value={formatCompact(data?.tasks_failed.value ?? 0)} note="needs attention" tone={(data?.tasks_failed.value ?? 0) > 0 ? "warning" : "positive"} />
        <ControlRoomKpi label="Active agents" value={formatCompact(data?.agents_active.value ?? 0)} note="contributors" />
        <ControlRoomKpi label="Active members" value={formatCompact(data?.members_active.value ?? 0)} note="human collaborators" />
        <ControlRoomKpi label="Runtime cost" value={formatDollars(data?.spend_cents.value ?? 0)} note="exact charges plus token estimates" />
      </section>

      <div className="grid gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(300px,1fr)]">
        <ControlRoomPanel title="Work throughput" meta="Completed tasks and issues · click a day to filter every analytics panel">
          {isLoading ? <ControlRoomLoading /> : (data?.timeline.length ?? 0) === 0 ? <ControlRoomEmpty>No activity in the selected range.</ControlRoomEmpty> : (
            <div className="grid min-h-52 grid-flow-col auto-cols-fr items-end gap-1 p-4" role="list" aria-label="Daily work throughput">
              {data?.timeline.map((bucket) => {
                const total = bucket.tasks_completed + bucket.issues_done;
                const height = Math.max(8, Math.round((total / maxThroughput) * 100));
                return (
                  <button
                    key={bucket.day}
                    type="button"
                    onClick={() => filterDay(bucket.day)}
                    className="group flex h-full min-w-0 flex-col justify-end gap-2 rounded px-1 hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={`Filter Dashboard by ${bucket.day}: ${total} completed`}
                  >
                    <span className="w-full rounded-t bg-primary/75 transition-colors group-hover:bg-primary" style={{ height: `${height}%` }} />
                    <span className="truncate text-[9px] text-muted-foreground">{bucket.day.slice(5)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </ControlRoomPanel>

        <ControlRoomPanel title="Issue outcomes" meta="Click a status to filter all analytics panels">
          <RankedBuckets
            rows={data?.issues_by_status ?? []}
            loading={isLoading}
            onSelect={(value) => addFilter("status", value, "in")}
          />
        </ControlRoomPanel>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <ControlRoomPanel title="People driving work" meta="Agents · click a person to filter the complete Dashboard">
          <PeopleRows rows={data?.top_agents ?? []} loading={isLoading} actorType="agent" onSelect={onSelectActor} />
        </ControlRoomPanel>
        <ControlRoomPanel title="Human collaboration" meta="Members · click a person to filter the complete Dashboard">
          <PeopleRows rows={data?.top_members ?? []} loading={isLoading} actorType="member" onSelect={onSelectActor} />
        </ControlRoomPanel>
      </div>

      <AnalyticsDashboard
        workspaceId={workspaceId}
        initialVisuals={DEFAULT_ANALYTICS_VISUALS.filter((visual) => visual.id !== "runs")}
        filters={filters}
        onFiltersChange={onFiltersChange}
        showToolbar={false}
        builderOpen={builderOpen}
        onBuilderOpenChange={onBuilderOpenChange}
      />
    </div>
  );
}

function RankedBuckets({ rows, loading, onSelect }: { rows: { key: string; count: number }[]; loading: boolean; onSelect: (key: string) => void }) {
  if (loading) return <ControlRoomLoading />;
  if (rows.length === 0) return <ControlRoomEmpty>No issue outcomes in the selected range.</ControlRoomEmpty>;
  const maximum = Math.max(...rows.map((row) => row.count), 1);
  return (
    <div className="space-y-1 p-3">
      {rows.map((row) => (
        <button key={row.key} type="button" onClick={() => onSelect(row.key)} className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Filter Dashboard by status ${row.key}`}>
          <span className="min-w-0">
            <span className="mb-1 block truncate text-xs font-medium capitalize">{row.key.replaceAll("_", " ")}</span>
            <MetricBar value={row.count} maximum={maximum} />
          </span>
          <span className="font-mono text-xs tabular-nums">{formatCompact(row.count)}</span>
        </button>
      ))}
    </div>
  );
}

function PeopleRows({ rows, loading, actorType, onSelect }: { rows: TopActor[]; loading: boolean; actorType: "member" | "agent"; onSelect: (id: string, name: string, actorType: "member" | "agent") => void }) {
  if (loading) return <ControlRoomLoading />;
  if (rows.length === 0) return <ControlRoomEmpty>No people activity in the selected range.</ControlRoomEmpty>;
  const maximum = Math.max(...rows.map((row) => row.count), 1);
  return (
    <div className="space-y-1 p-3">
      {rows.map((row) => (
        <button key={row.id} type="button" onClick={() => onSelect(row.id, row.name, actorType)} className="grid w-full grid-cols-[28px_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Filter Dashboard by ${row.name}`}>
          <span className="flex size-7 items-center justify-center rounded-full bg-primary/10 text-[10px] font-semibold text-primary">{row.name.slice(0, 1).toUpperCase()}</span>
          <span className="min-w-0">
            <span className="mb-1 block truncate text-xs font-medium">{row.name}</span>
            <MetricBar value={row.count} maximum={maximum} />
          </span>
          <span className="font-mono text-xs tabular-nums">{formatCompact(row.count)}</span>
        </button>
      ))}
    </div>
  );
}
