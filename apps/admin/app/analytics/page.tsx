"use client";

import { useMemo, useState } from "react";
import { useAnalytics } from "@/lib/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { KpiRow } from "@/components/analytics/kpi-row";
import { ErrorsChart } from "@/components/analytics/errors-chart";
import { AutopilotRunsChart } from "@/components/analytics/autopilot-runs-chart";
import { WorkspaceBreakdownSheet } from "@/components/analytics/workspace-breakdown-sheet";
import { CountsChart } from "@/components/analytics/counts-chart";
import {
  WindowToolbar,
  DEFAULT_WINDOW_HOURS,
  DEFAULT_GRANULARITY_HOURS,
  granularityOptionsFor,
  type WindowHours,
} from "@/components/analytics/window-toolbar";
import type { AnalyticsWorkspaceBreakdownParams, GranularityHours } from "@/lib/types";

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <h2 className="mb-3 text-body font-medium text-foreground">{title}</h2>
      {children}
    </div>
  );
}

export default function AnalyticsPage() {
  const [windowHours, setWindowHours] = useState<WindowHours>(DEFAULT_WINDOW_HOURS);
  const [granularityHours, setGranularityHours] = useState<GranularityHours>(DEFAULT_GRANULARITY_HOURS);
  const [workspaceBreakdown, setWorkspaceBreakdown] = useState<AnalyticsWorkspaceBreakdownParams | null>(null);

  // Mirrors usage-section.tsx's handleDimChange: reset granularity to the
  // window's first valid option whenever it's no longer offered.
  function handleWindowChange(hours: WindowHours) {
    setWindowHours(hours);
    const choices = granularityOptionsFor(hours);
    if (!choices.includes(granularityHours)) {
      // Every WindowHours key maps to a non-empty options array (see
      // GRANULARITY_OPTIONS in window-toolbar.tsx) — choices[0] always exists.
      setGranularityHours(choices[0]!);
    }
  }

  const { from, to } = useMemo(() => {
    const now = new Date();
    const to = now.toISOString();
    const from = new Date(now.getTime() - windowHours * 3_600_000).toISOString();
    return { from, to };
  }, [windowHours]);

  const params = useMemo(() => ({ from, to, granularityHours }), [from, to, granularityHours]);
  const { data, isLoading, isError, isFetching, refetch } = useAnalytics(params);

  function openWorkspaceBreakdown(kind: AnalyticsWorkspaceBreakdownParams["kind"], segment: string, bucketStart: string) {
    const bucketEnd = Math.min(
      new Date(bucketStart).getTime() + granularityHours * 3_600_000,
      new Date(to).getTime(),
    );
    setWorkspaceBreakdown({ kind, segment, from: bucketStart, to: new Date(bucketEnd).toISOString() });
  }

  return (
    <main className="mx-auto max-w-7xl px-6 py-8">
      <div className="mb-6">
        <h1 className="text-display-sm font-medium text-foreground">Analytics</h1>
        <p className="mt-1 text-body text-muted-foreground">
          Platform-wide trends across every workspace.
        </p>
      </div>

      <div className="mb-6">
        <WindowToolbar
          windowHours={windowHours}
          onWindowChange={handleWindowChange}
          granularityHours={granularityHours}
          onGranularityChange={setGranularityHours}
        />
      </div>

      {isError ? (
        <div className="flex flex-col items-center gap-3 py-10 text-center">
          <p className="text-destructive">Failed to load analytics.</p>
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            Retry
          </Button>
        </div>
      ) : isLoading && !data ? (
        <p className="py-10 text-center text-muted-foreground">Loading analytics…</p>
      ) : data ? (
        <div className="flex flex-col gap-6">
          <KpiRow result={data} />

          <ChartCard title="Errors">
            <ErrorsChart buckets={data.buckets} onSegmentClick={isFetching ? undefined : (bucketStart, segment) => openWorkspaceBreakdown("errors", segment, bucketStart)} />
          </ChartCard>

          <ChartCard title="Autopilot runs">
            <AutopilotRunsChart buckets={data.buckets} onSegmentClick={isFetching ? undefined : (bucketStart, segment) => openWorkspaceBreakdown("autopilotRuns", segment, bucketStart)} />
          </ChartCard>

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <ChartCard title="Workspaces created">
              <CountsChart buckets={data.buckets} metric="workspacesCreated" seriesLabel="Workspaces" />
            </ChartCard>
            <ChartCard title="Issues created">
              <CountsChart
                buckets={data.buckets}
                metric="issuesCreated"
                seriesLabel="Issues"
                color="var(--chart-2)"
              />
            </ChartCard>
          </div>
        </div>
      ) : null}

      <WorkspaceBreakdownSheet selection={workspaceBreakdown} onClose={() => setWorkspaceBreakdown(null)} />
    </main>
  );
}
