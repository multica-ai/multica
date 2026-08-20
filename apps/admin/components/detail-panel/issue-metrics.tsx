import { Bar, BarChart, XAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { IssueMetrics } from "@/lib/types";

// Single-series trend, one consistent color — no per-label breakdown here
// (that's the pills above), just a real hover tooltip on the day/count bars.
const issuesChartConfig = {
  count: { label: "Issues created", color: "var(--chart-1)" },
} satisfies ChartConfig;

function formatDayLabel(iso: string): string {
  return new Date(`${iso}T00:00:00Z`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

export function IssueMetricsSection({ issues }: { issues: IssueMetrics }) {
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
        <ChartContainer
          config={issuesChartConfig}
          className="aspect-auto h-16 w-full"
          role="img"
          aria-label="Issues created over the last 14 days"
        >
          <BarChart
            data={issues.dailyOpenCounts.map((d) => ({ ...d, label: formatDayLabel(d.date) }))}
            margin={{ left: 0, right: 0, top: 4, bottom: 0 }}
          >
            <XAxis dataKey="label" tickLine={false} axisLine={false} hide />
            <ChartTooltip content={<ChartTooltipContent hideIndicator />} />
            <Bar dataKey="count" fill="var(--color-count)" radius={[2, 2, 0, 0]} />
          </BarChart>
        </ChartContainer>
      )}
    </section>
  );
}
