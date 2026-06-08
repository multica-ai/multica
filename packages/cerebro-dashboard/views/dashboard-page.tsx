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
import { IssuesOnBehalfOf } from "./components/issues-on-behalf-of";
import { TopActors } from "./components/top-actors";
import { ActivityFeed } from "./components/activity-feed";
import { RecentTasksList } from "./components/recent-tasks-list";
import { MessageTracker } from "./components/message-tracker";
import { MessageFlow } from "./components/message-flow";
import { ActorMessagePanel } from "./components/actor-message-panel";
import { AllMessagesTable } from "./components/all-messages-table";
import { DashboardTabBar } from "./components/dashboard-tab-bar";
import { useDashboardStore } from "../core/store";
import { dashboardOverviewOptions } from "../core/queries";

// Workspace operations dashboard. JEH-684. v2 — replaces the v1 placeholder
// version with a single /api/cerebro/dashboard overview query that drives
// every panel: KPIs (with prior-period delta), per-day activity chart,
// issues-by-status/priority donuts, top actors, activity feed, recent tasks.
// TECH-3093: two tabs — Issues (original) and Messages (senders/recipients/flow).
export function DashboardPage() {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const workspace = useCurrentWorkspace();
  const range = useDashboardStore((s) => s.range);
  const scope = useDashboardStore((s) => s.scope);
  const actorId = useDashboardStore((s) => s.actorId);
  const actorName = useDashboardStore((s) => s.actorName);
  const tab = useDashboardStore((s) => s.tab);
  const setActor = useDashboardStore((s) => s.setActor);

  const wsId = workspace?.id ?? "";
  const overview = useQuery(dashboardOverviewOptions(wsId, range, scope, actorId));

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
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex min-w-0 flex-col">
            <h1 className="text-sm font-semibold">Dashboard</h1>
            <p className="truncate text-[11px] text-muted-foreground">
              {data
                ? `${formatPeriodLabel(data.period_start, data.period_end)} — ${actorName ?? scopeLabel(scope)}`
                : "Henter overblik…"}
            </p>
          </div>
          <DashboardTabBar />
        </div>
        <div className="flex items-center gap-2">
          {actorId && (
            <button
              type="button"
              onClick={() => setActor(null)}
              className="rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
            >
              {actorName ?? "Actor"} x
            </button>
          )}
          <ActorTabs />
          <TimeRangePicker />
        </div>
      </PageHeader>

      {tab === "messages" && <ActorMessagePanel wsId={workspace.id} />}

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-6 p-6">
          {overview.isError && (
            <p className="text-xs text-destructive">
              Kunne ikke hente dashboard:{" "}
              {overview.error instanceof Error ? overview.error.message : "ukendt fejl"}
            </p>
          )}

          {tab === "issues" && (
            <>
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

          {tab === "messages" && (
            <>
              <section aria-label="Message KPIs">
                <KpiCards
                  data={data}
                  isLoading={overview.isLoading}
                  workspaceSlug={workspace.slug}
                  wsId={workspace.id}
                  filter="messages"
                />
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
                <AllMessagesTable wsId={workspace.id} />
              </section>
            </>
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

function scopeLabel(scope: "all" | "members" | "agents"): string {
  if (scope === "members") return "Members";
  if (scope === "agents") return "Agents";
  return "Alle";
}
