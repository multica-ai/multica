"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import type { Autopilot, DashboardUsageBySource } from "@multica/core/types";
import { formatTokens, formatUsd } from "../../runtimes/utils";
import { formatDateLabel } from "../utils";
import { useT } from "../../i18n";
import {
  sourceBuckets,
  sourceTotals,
  usageByAutopilot,
  type SourceMetric,
} from "../usage-source";
import { Segmented, type Dim } from "./dashboard-shared";
import { DimSegmented } from "./dim-segmented";

const TOP_AUTOPILOTS = 8;

/**
 * Manual runs vs autopilot runs: how much of the spend is scheduled
 * automation, and which autopilots account for it. A run belongs to an
 * autopilot when the autopilot started it or created the issue it ran on.
 */
export function UsageSourceCard({
  rows,
  autopilots,
  allowedDims,
}: {
  rows: readonly DashboardUsageBySource[];
  autopilots: readonly Autopilot[];
  allowedDims: readonly Dim[];
}) {
  const { t } = useT("usage");
  const [metric, setMetric] = useState<SourceMetric>("tokens");
  const [dim, setDim] = useState<Dim>("daily");
  const effectiveDim: Dim = allowedDims.includes(dim) ? dim : allowedDims[0]!;

  const totals = useMemo(() => sourceTotals(rows), [rows]);
  const buckets = useMemo(
    () => sourceBuckets(rows, metric, effectiveDim === "weekly"),
    [rows, metric, effectiveDim],
  );
  const ranked = useMemo(() => usageByAutopilot(rows, metric), [rows, metric]);
  const names = useMemo(() => new Map(autopilots.map((a) => [a.id, a.title])), [autopilots]);

  const format = metric === "cost" ? formatUsd : formatTokens;
  const manual = metric === "cost" ? totals.manualCost : totals.manualTokens;
  const autopilot = metric === "cost" ? totals.autopilotCost : totals.autopilotTokens;
  const total = manual + autopilot;
  const share = total > 0 ? Math.round((autopilot / total) * 100) : 0;

  const config = {
    manual: { label: t(($) => $.source.manual), color: "var(--chart-1)" },
    autopilot: { label: t(($) => $.source.autopilot), color: "var(--chart-3)" },
  } satisfies ChartConfig;
  const chartData = buckets.map((b) => ({ ...b, label: formatDateLabel(b.date) }));

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-body font-semibold">{t(($) => $.source.title)}</h4>
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            label={t(($) => $.daily.metric_label)}
            value={metric}
            onChange={setMetric}
            options={[
              { label: t(($) => $.daily.metric_tokens), value: "tokens" as const },
              { label: t(($) => $.daily.metric_cost), value: "cost" as const },
            ]}
          />
          <DimSegmented allowedDims={allowedDims} value={effectiveDim} onChange={setDim} />
        </div>
      </div>

      <dl className="mb-3 flex flex-wrap gap-x-6 gap-y-1 text-caption">
        <div className="flex items-center gap-1.5">
          <span aria-hidden className="size-2 rounded-full" style={{ background: config.manual.color }} />
          <dt className="text-muted-foreground">{config.manual.label}</dt>
          <dd className="font-medium tabular-nums">{format(manual)}</dd>
        </div>
        <div className="flex items-center gap-1.5">
          <span aria-hidden className="size-2 rounded-full" style={{ background: config.autopilot.color }} />
          <dt className="text-muted-foreground">{config.autopilot.label}</dt>
          <dd className="font-medium tabular-nums">{format(autopilot)}</dd>
        </div>
        <div className="flex items-center gap-1.5">
          <dt className="text-muted-foreground">{t(($) => $.source.share)}</dt>
          <dd className="font-medium tabular-nums">{share}%</dd>
        </div>
      </dl>

      <ChartContainer config={config} className="aspect-[4/1] w-full">
        <BarChart data={chartData} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
          <YAxis tickLine={false} axisLine={false} tickMargin={8} width="auto" tickFormatter={(v: number) => format(v)} />
          <ChartTooltip
            content={
              <ChartTooltipContent
                formatter={(value, name) =>
                  typeof value === "number" ? `${format(value)} ${name}` : `${value} ${name}`
                }
              />
            }
          />
          <Bar dataKey="manual" stackId="source" fill="var(--color-manual)" />
          <Bar dataKey="autopilot" stackId="source" fill="var(--color-autopilot)" radius={[3, 3, 0, 0]} />
        </BarChart>
      </ChartContainer>

      {ranked.length > 0 && (
        <Table className="mt-3">
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.source.autopilot_col)}</TableHead>
              <TableHead className="text-right">{t(($) => $.daily.metric_tokens)}</TableHead>
              <TableHead className="text-right">{t(($) => $.daily.metric_cost)}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {ranked.slice(0, TOP_AUTOPILOTS).map((r) => (
              <TableRow key={r.autopilotId}>
                <TableCell className="max-w-72 truncate">
                  {names.get(r.autopilotId) ?? t(($) => $.source.deleted_autopilot)}
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatTokens(r.tokens)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatUsd(r.cost)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
