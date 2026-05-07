"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorTabs } from "./components/actor-tabs";
import { TimeRangePicker } from "./components/time-range-picker";
import { AgentStrip } from "./components/agent-strip";
import { KpiCards } from "./components/kpi-cards";
import { RecentTasksList } from "./components/recent-tasks-list";

// Workspace operations dashboard. JEH-684. Built as a cerebro extension
// (packages/cerebro-dashboard) so the only upstream-zone touch is a single
// CEREBRO-PATCH in app-sidebar.tsx that mounts <DashboardNavItem />.
//
// Fase 1 ships skeleton + KPI cards + recent tasks. Charts and activity
// feed are placeholders rendered below; they are wired up in JEH-703 (Fase 2)
// and JEH-704 (Fase 3).
export function DashboardPage() {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const workspace = useCurrentWorkspace();

  if (!enabled) {
    return null;
  }

  if (!workspace) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-semibold">Dashboard</h1>
          <p className="text-xs text-muted-foreground">
            Overblik over hvad der sker i forretningen
          </p>
        </div>
        <div className="flex items-center gap-3">
          <ActorTabs />
          <TimeRangePicker />
        </div>
      </header>

      <section aria-label="Agents">
        <AgentStrip wsId={workspace.id} />
      </section>

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
