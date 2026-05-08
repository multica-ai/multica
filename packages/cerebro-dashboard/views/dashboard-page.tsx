"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { ActorTabs } from "./components/actor-tabs";
import { TimeRangePicker } from "./components/time-range-picker";
import { KpiCards } from "./components/kpi-cards";
import { ActivityChart } from "./components/activity-chart";
import { IssuesDonut } from "./components/issues-donut";
import { TopActors } from "./components/top-actors";
import { ActivityFeed } from "./components/activity-feed";
import { RecentTasksList } from "./components/recent-tasks-list";
import { useDashboardStore } from "../core/store";
import { dashboardOverviewOptions } from "../core/queries";

// Workspace operations dashboard. JEH-684. v2 — replaces the v1 placeholder
// version with a single /api/cerebro/dashboard overview query that drives
// every panel: KPIs (with prior-period delta), per-day activity chart,
// issues-by-status/priority donuts, top actors, activity feed, recent tasks.
export function DashboardPage() {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const workspace = useCurrentWorkspace();
  const range = useDashboardStore((s) => s.range);
  const scope = useDashboardStore((s) => s.scope);

  const wsId = workspace?.id ?? "";
  const overview = useQuery(dashboardOverviewOptions(wsId, range));

  if (!enabled) return null;

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const data = overview.data;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Dashboard</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            {data
              ? `${formatPeriodLabel(data.period_start, data.period_end)} — overblik`
              : "Henter overblik…"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <ActorTabs />
          <TimeRangePicker />
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-6 p-6">
          <section aria-label="KPIs">
            <KpiCards
              data={data}
              isLoading={overview.isLoading}
              workspaceSlug={workspace.slug}
              wsId={workspace.id}
            />
          </section>

          <section aria-label="Activity over time" className="grid gap-3 lg:grid-cols-3">
            <div className="lg:col-span-2">
              <ActivityChart data={data} isLoading={overview.isLoading} />
            </div>
            <IssuesDonut data={data} isLoading={overview.isLoading} kind="status" />
          </section>

          <section aria-label="Breakdown" className="grid gap-3 lg:grid-cols-3">
            <IssuesDonut data={data} isLoading={overview.isLoading} kind="priority" />
            <TopActors data={data} isLoading={overview.isLoading} kind="agents" />
            <TopActors data={data} isLoading={overview.isLoading} kind="members" />
          </section>

          <section aria-label="Recent" className="grid gap-3 lg:grid-cols-2">
            <ActivityFeed data={data} isLoading={overview.isLoading} scope={scope} />
            <RecentTasksList data={data} isLoading={overview.isLoading} workspaceSlug={workspace.slug} />
          </section>

          {overview.isError && (
            <p className="text-xs text-destructive">
              Kunne ikke hente dashboard:{" "}
              {overview.error instanceof Error ? overview.error.message : "ukendt fejl"}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

function formatPeriodLabel(start: string, end: string): string {
  try {
    const s = new Date(start);
    const e = new Date(end);
    const fmt = (d: Date) =>
      d.toLocaleDateString("da-DK", { day: "2-digit", month: "short" });
    return `${fmt(s)} – ${fmt(e)}`;
  } catch {
    return "";
  }
}
