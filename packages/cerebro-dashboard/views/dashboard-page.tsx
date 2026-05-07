"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorTabs } from "./components/actor-tabs";
import { TimeRangePicker } from "./components/time-range-picker";
import { AgentStrip } from "./components/agent-strip";
import { MemberStrip } from "./components/member-strip";
import { KpiCards } from "./components/kpi-cards";
import { RecentTasksList } from "./components/recent-tasks-list";
import { useDashboardStore } from "../core/store";

// Workspace operations dashboard. JEH-684. Fase 1 ships skeleton + KPI cards
// + recent tasks; charts and activity feed are placeholders. Layout follows
// the SidebarInset pattern: PageHeader at top (sticky), scrollable content
// below — matches the inbox/agents/issues pages.
export function DashboardPage() {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const workspace = useCurrentWorkspace();
  const scope = useDashboardStore((s) => s.scope);

  if (!enabled) {
    return null;
  }

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Dashboard</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Overblik over hvad der sker i forretningen
          </p>
        </div>
        <div className="flex items-center gap-2">
          <ActorTabs />
          <TimeRangePicker />
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-6 p-6">
          {scope !== "members" && (
            <section aria-label="Agents">
              <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                Agents
              </h2>
              <AgentStrip wsId={workspace.id} />
            </section>
          )}

          {scope !== "agents" && (
            <section aria-label="Members">
              <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                Members
              </h2>
              <MemberStrip wsId={workspace.id} />
            </section>
          )}

          <section aria-label="KPIs">
            <KpiCards wsId={workspace.id} workspaceSlug={workspace.slug} />
          </section>

          <section aria-label="Charts" className="grid gap-3 lg:grid-cols-2">
            <PlaceholderCard
              title="Run activity"
              hint="Line chart over agent task throughput. Kommer i Fase 3 (JEH-704)."
            />
            <PlaceholderCard
              title="Issues by priority"
              hint="Donut. Kommer i Fase 3 (JEH-704)."
            />
            <PlaceholderCard
              title="Issues by status"
              hint="Donut. Kommer i Fase 3 (JEH-704)."
            />
            <PlaceholderCard
              title="Success rate"
              hint="completed / (completed + failed). Kommer i Fase 3 (JEH-704)."
            />
          </section>

          <section aria-label="Detail" className="grid gap-3 lg:grid-cols-2">
            <PlaceholderCard
              title="Recent activity"
              hint="Activity feed fra activity_log. Kommer i Fase 2 (JEH-703)."
            />
            <RecentTasksList wsId={workspace.id} />
          </section>
        </div>
      </div>
    </div>
  );
}

function PlaceholderCard({ title, hint }: { title: string; hint: string }) {
  return (
    <Card className="border-dashed">
      <div className="border-b border-dashed px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {title}
        </h3>
      </div>
      <CardContent className="flex h-32 items-center justify-center p-3 text-center text-xs text-muted-foreground">
        {hint}
      </CardContent>
    </Card>
  );
}
