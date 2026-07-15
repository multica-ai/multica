"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { DashboardTabBar } from "./components/dashboard-tab-bar";
import { useDashboardStore } from "../core/store";
import { dashboardOverviewOptions } from "../core/queries";
import { AnalyticsDashboard } from "./components/analytics-dashboard";
import { MessagesControlRoom } from "./components/messages-control-room";
import { OverviewControlRoom } from "./components/overview-control-room";
import { RunsControlRoom } from "./components/runs-control-room";
import { filtersFromSearchParams, type AnalyticsFilter } from "../core/analytics";

// One Dashboard shell and filter state for the three FIR-2996 control rooms.
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
          {actorId && (
            <button
              type="button"
              onClick={() => setActor(null)}
              className="rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
            >
              {actorName ?? "Actor"} x
            </button>
          )}
          <DashboardTabBar />
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex min-h-full flex-col">
          {overview.isError && (
            <p className="mx-6 mt-4 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              Failed to load dashboard:{" "}
              {overview.error instanceof Error ? overview.error.message : "unknown error"}
            </p>
          )}

          {tab === "overview" && <OverviewControlRoom workspaceId={workspace.id} data={data} isLoading={overview.isLoading} filters={analyticsFilters} onFiltersChange={setAnalyticsFilters} onNewVisual={() => setVisualBuilderOpen(true)} onSelectActor={(id, name) => setActor(id, name)} builderOpen={visualBuilderOpen} onBuilderOpenChange={setVisualBuilderOpen} />}

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

          {tab === "messages" && <MessagesControlRoom workspaceId={workspace.id} workspaceSlug={workspace.slug} data={data} isLoading={overview.isLoading} filters={analyticsFilters} onFiltersChange={setAnalyticsFilters} onNewVisual={() => setVisualBuilderOpen(true)} onSelectActor={(id, name) => setActor(id, name)} builderOpen={visualBuilderOpen} onBuilderOpenChange={setVisualBuilderOpen} />}
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
