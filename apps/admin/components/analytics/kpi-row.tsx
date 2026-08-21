import { formatCost } from "@/lib/format";
import type { AnalyticsResult, AnalyticsBucket } from "@/lib/types";

function sumBy(buckets: AnalyticsBucket[], pick: (b: AnalyticsBucket) => number): number {
  return buckets.reduce((s, b) => s + pick(b), 0);
}

function sumErrors(buckets: AnalyticsBucket[]): number {
  return sumBy(buckets, (b) => Object.values(b.errors).reduce((a, c) => a + c, 0));
}

function sumAutopilotRuns(buckets: AnalyticsBucket[]): number {
  return sumBy(
    buckets,
    (b) => b.autopilotRuns.completed + b.autopilotRuns.failed + b.autopilotRuns.skipped + b.autopilotRuns.other,
  );
}

/**
 * KPI strip above the charts. LiteLLM spend is deliberately a lifetime total
 * (see AnalyticsResult.totalLiteLlmSpendUsd's doc comment) — every other
 * card sums across the buckets currently in view, so it moves with the
 * window/granularity toolbar.
 */
export function KpiRow({ result }: { result: AnalyticsResult }) {
  return (
    <div className="grid grid-cols-2 divide-x divide-y rounded-lg border bg-card sm:grid-cols-3 lg:grid-cols-5 lg:divide-y-0">
      <Kpi label="LiteLLM spend (lifetime)" value={formatCost(result.totalLiteLlmSpendUsd)} />
      <Kpi label="Workspaces created" value={sumBy(result.buckets, (b) => b.workspacesCreated).toLocaleString()} />
      <Kpi label="Issues created" value={sumBy(result.buckets, (b) => b.issuesCreated).toLocaleString()} />
      <Kpi label="Autopilot runs" value={sumAutopilotRuns(result.buckets).toLocaleString()} />
      <Kpi
        label="Errors"
        value={sumErrors(result.buckets).toLocaleString()}
        accent={sumErrors(result.buckets) > 0 ? "warning" : undefined}
      />
    </div>
  );
}

function Kpi({ label, value, accent }: { label: string; value: string; accent?: "warning" }) {
  return (
    <div className="flex flex-col gap-1.5 p-4">
      <div className="text-micro font-medium uppercase tracking-wider text-muted-foreground">{label}</div>
      <div
        className={`text-display-sm font-semibold leading-none tabular-nums ${accent === "warning" ? "text-warning" : ""}`}
      >
        {value}
      </div>
    </div>
  );
}
