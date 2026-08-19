import type { IssueMetrics } from "@/lib/types";

// A 14-bar trend of real daily issue-creation counts (lib/queries.ts's
// getIssueMetrics). Hand-rolled with plain divs rather than the shared
// recharts-based ChartContainer (packages/ui/components/ui/chart.tsx):
// this is a single fixed-shape sparkline with no interactivity, legend, or
// tooltip requirement, so pulling in a charting library for it would be
// speculative generality against a one-off use.
export function IssueMetricsSection({ issues }: { issues: IssueMetrics }) {
  const max = Math.max(1, ...issues.dailyOpenCounts.map((d) => d.count));

  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Issues
      </h3>
      <div className="mb-4 grid grid-cols-3 gap-3">
        <div>
          <p className="text-title-lg font-medium text-foreground">{issues.openIssues}</p>
          <p className="text-caption text-muted-foreground">Open</p>
        </div>
        <div>
          <p className="text-title-lg font-medium text-foreground">{issues.closedLast7d}</p>
          <p className="text-caption text-muted-foreground">Closed (7d)</p>
        </div>
        <div>
          <p className="text-title-lg font-medium text-foreground">
            {issues.avgResolutionHours !== null ? `${issues.avgResolutionHours}h` : "—"}
          </p>
          <p className="text-caption text-muted-foreground">Avg. resolution</p>
        </div>
      </div>
      {issues.labelBreakdown.length > 0 && (
        // Plan §2.2E: "Open Issues: 12 (with severity breakdown by label)".
        <ul className="mb-4 flex flex-wrap gap-1.5">
          {issues.labelBreakdown.map((label) => (
            <li
              key={label.name}
              className="flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-caption text-foreground"
            >
              <span
                className="size-1.5 rounded-full"
                style={{ backgroundColor: label.color }}
                aria-hidden
              />
              {label.name} · {label.count}
            </li>
          ))}
        </ul>
      )}
      {issues.dailyOpenCounts.length === 0 ? (
        <p className="text-body text-muted-foreground">No issues created in the last 14 days.</p>
      ) : (
        <div className="flex h-16 items-end gap-1" role="img" aria-label="Issues created over the last 14 days">
          {issues.dailyOpenCounts.map((day) => (
            <div
              key={day.date}
              title={`${day.date}: ${day.count}`}
              className="flex-1 rounded-sm bg-primary/70"
              style={{ height: `${Math.max(4, (day.count / max) * 100)}%` }}
            />
          ))}
        </div>
      )}
    </section>
  );
}
