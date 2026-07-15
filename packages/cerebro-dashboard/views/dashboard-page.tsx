"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { ActorTabs } from "./components/actor-tabs";
import { TimeRangePicker } from "./components/time-range-picker";
import { KpiCards } from "./components/kpi-cards";
import { ActivityChart } from "./components/activity-chart";
import { IssuesDonut } from "./components/issues-donut";
import { IssuesOnBehalfOf } from "./components/issues-on-behalf-of";
import { TopActors } from "./components/top-actors";
import { ActivityFeed } from "./components/activity-feed";
import { RecentTasksList } from "./components/recent-tasks-list";
import { MessageTracker } from "./components/message-tracker";
import { MessageFlow } from "./components/message-flow";
import { ActorMessagePanel } from "./components/actor-message-panel";
import { AllMessagesTable } from "./components/all-messages-table";
import { MessageSearchPanel } from "./components/message-search-panel";
import { MessageActivityChart } from "./components/message-activity-chart";
import { MessageSpendTable } from "./components/message-spend-table";
import { DashboardTabBar } from "./components/dashboard-tab-bar";
import { useDashboardStore } from "../core/store";
import { dashboardOverviewOptions } from "../core/queries";
import { AnalyticsDashboard } from "./components/analytics-dashboard";
import { RunsControlRoom } from "./components/runs-control-room";
import { DEFAULT_ANALYTICS_VISUALS, filtersFromSearchParams, type AnalyticsFilter } from "../core/analytics";

// Workspace operations dashboard. JEH-684. v2 — replaces the v1 placeholder
// version with a single /api/cerebro/dashboard overview query that drives
// every panel: KPIs (with prior-period delta), per-day activity chart,
// issues-by-status/priority donuts, top actors, activity feed, recent tasks.
// TECH-3093: two tabs — Issues (original) and Messages (senders/recipients/flow/spend/chart).
export function DashboardPage() {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const workspace = useCurrentWorkspace();
  const range = useDashboardStore((s) => s.range);
  const scope = useDashboardStore((s) => s.scope);
  const actorId = useDashboardStore((s) => s.actorId);
  const actorName = useDashboardStore((s) => s.actorName);
  const tab = useDashboardStore((s) => s.tab);
  const setActor = useDashboardStore((s) => s.setActor);
  const [visualBuilderOpen, setVisualBuilderOpen] = useState(false);
  const [analyticsFilters, setAnalyticsFilters] = useState<AnalyticsFilter[]>(() =>
    typeof window === "undefined" ? [] : filtersFromSearchParams(new URLSearchParams(window.location.search)),
  );
  const wsId = workspace?.id ?? "";
  const overview = useQuery(dashboardOverviewOptions(wsId, range, scope, actorId));

  if (!enabled) return null;

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading workspace context…
      </div>
    );
  }

  const data = overview.data;

  return (
    <div data-testid="dashboard-content" className="flex h-full flex-col bg-[#fbfbfa] text-[#242228]">
      <PageHeader className="justify-between gap-3">
        <div className="flex shrink-0 items-center gap-3">
          <div className="flex min-w-0 flex-col">
            <h1 className="text-sm font-semibold">Dashboard</h1>
            <p className="truncate text-[11px] text-muted-foreground">
              {data
                ? tab === "runs"
                  ? `Workspace operations · ${formatPeriodLabel(data.period_start, data.period_end)}`
                  : `${formatPeriodLabel(data.period_start, data.period_end)} — ${actorName ?? scopeLabel(scope)}`
                : "Loading overview…"}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {tab !== "runs" && actorId && (
            <button
              type="button"
              onClick={() => setActor(null)}
              className="rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
            >
              {actorName ?? "Actor"} x
            </button>
          )}
          {tab !== "runs" && <ActorTabs />}
          {tab !== "runs" && <TimeRangePicker />}
          <DashboardTabBar />
        </div>
      </PageHeader>

      {tab === "messages" && <ActorMessagePanel wsId={workspace.id} workspaceSlug={workspace.slug} />}

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className={tab === "runs" ? "flex min-h-full flex-col" : "flex flex-col gap-6 p-6"}>
          {overview.isError && (
            <p className="text-xs text-destructive">
              Failed to load dashboard:{" "}
              {overview.error instanceof Error ? overview.error.message : "unknown error"}
            </p>
          )}

          {tab === "overview" && (
            <>
              <AnalyticsDashboard
                workspaceId={workspace.id}
                initialVisuals={DEFAULT_ANALYTICS_VISUALS.filter((visual) => visual.id !== "runs")}
                filters={analyticsFilters}
                onFiltersChange={setAnalyticsFilters}
              />

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

              <section aria-label="On behalf of" className="grid gap-3">
                <IssuesOnBehalfOf data={data} isLoading={overview.isLoading} />
              </section>

              <section aria-label="Recent" className="grid gap-3 lg:grid-cols-2">
                <ActivityFeed
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                />
                <RecentTasksList
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                />
              </section>
            </>
          )}

          {tab === "runs" && (
            <>
              <div className={visualBuilderOpen ? "pr-[350px]" : undefined}>
                <RunsControlRoom
                  workspaceId={workspace.id}
                  filters={analyticsFilters}
                  onFiltersChange={setAnalyticsFilters}
                  onNewVisual={() => setVisualBuilderOpen(true)}
                  onRunPanelOpen={() => setVisualBuilderOpen(false)}
                />
              </div>
              <div className={`px-6 pb-6 ${visualBuilderOpen ? "pr-[374px]" : ""}`}>
                <AnalyticsDashboard
                  workspaceId={workspace.id}
                  initialVisuals={[]}
                  filters={analyticsFilters}
                  onFiltersChange={setAnalyticsFilters}
                  showToolbar={false}
                  builderOpen={visualBuilderOpen}
                  onBuilderOpenChange={setVisualBuilderOpen}
                />
              </div>
            </>
          )}

          {tab === "messages" && (
            <>
              <section aria-label="Search all conversations">
                <MessageSearchPanel workspaceSlug={workspace.slug} />
              </section>

              <section aria-label="Message KPIs">
                <KpiCards
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                  wsId={workspace.id}
                  filter="messages"
                />
              </section>

              <section aria-label="Message activity" className="grid gap-3 lg:grid-cols-2">
                <MessageActivityChart data={data} isLoading={overview.isLoading} />
                <MessageSpendTable data={data} isLoading={overview.isLoading} />
              </section>

              <section aria-label="Top senders and recipients">
                <MessageTracker
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                />
              </section>

              <section aria-label="Message flow">
                <MessageFlow
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                />
              </section>

              <section aria-label="All messages">
                <AllMessagesTable wsId={workspace.id} workspaceSlug={workspace.slug} />
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

export function formatPeriodLabel(start: string, end: string): string {
  try {
    const s = new Date(start);
    const e = new Date(end);
    const fmt = (d: Date) =>
      d.toLocaleDateString("en-GB", { day: "2-digit", month: "short", timeZone: "UTC" });
    return `${fmt(s)} – ${fmt(e)}`;
  } catch {
    return "";
  }
}

function scopeLabel(scope: "all" | "members" | "agents"): string {
  if (scope === "members") return "Members";
  if (scope === "agents") return "Agents";
  return "All";
}
