"use client";

import { useState } from "react";
import type { AnalyticsCatalog, AnalyticsDimension, AnalyticsOperator, AnalyticsQueryResult } from "@multica/cerebro-usage";
import type { AnalyticsFilter, AnalyticsVisual } from "../../core/analytics";
import { VisualBuilderDrawer } from "./visual-builder-drawer";

type Results = Record<string, AnalyticsQueryResult | undefined>;

interface AnalyticsWorkbenchProps {
  visuals: AnalyticsVisual[];
  results: Results;
  filters: AnalyticsFilter[];
  onFilter: (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in") => void;
  onRemoveFilter: (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => void;
  onNext: (visualId: string, cursor: string) => void;
  onPrevious?: (visualId: string) => void;
  canPrevious?: Record<string, boolean>;
  onAddVisual: (visual: AnalyticsVisual) => void;
  onConfigure?: (visual: AnalyticsVisual) => void;
  catalog?: AnalyticsCatalog;
  showToolbar?: boolean;
  builderOpen?: boolean;
  onBuilderOpenChange?: (open: boolean) => void;
}

const DIMENSIONS = new Set<AnalyticsDimension>([
  "time", "person", "agent", "project", "runtime", "source", "provider", "model", "skill", "status", "cost_kind", "quality_type", "quality_category", "context", "run", "source_id", "reference", "reference_label",
]);

export function AnalyticsWorkbench({ visuals, results, filters, onFilter, onRemoveFilter, onNext, onPrevious, canPrevious = {}, onAddVisual, onConfigure, catalog, showToolbar = true, builderOpen, onBuilderOpenChange }: AnalyticsWorkbenchProps) {
  const [internalCreating, setInternalCreating] = useState(false);
  const [editing, setEditing] = useState<AnalyticsVisual | null>(null);
  const creating = builderOpen ?? internalCreating;
  const setCreating = (open: boolean) => {
    if (builderOpen === undefined) setInternalCreating(open);
    onBuilderOpenChange?.(open);
  };
  return (
    <div className="space-y-4" aria-label="Analytics workspace">
      {showToolbar && <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-3">
        <div className="flex flex-wrap gap-1.5" aria-label="Active filters">
          {filters.length === 0 ? <span className="text-xs text-muted-foreground">All workspace activity</span> : filters.flatMap((filter) => filter.values.map((value) => (
            <button key={`${filter.dimension}:${filter.operator}:${value}`} type="button" onClick={() => onRemoveFilter(filter.dimension, value, filter.operator)} className="rounded border bg-muted/40 px-2 py-1 text-xs">
              {filter.dimension} {filter.operator === "not_in" ? "≠" : "="} {value} ×
            </button>
          )))}
        </div>
        <button type="button" aria-label="New visual" onClick={() => setCreating(true)} className="rounded-md border bg-background px-3 py-1.5 text-xs font-medium hover:bg-muted">New visual</button>
      </div>}

      <div className="grid gap-3 xl:grid-cols-2">
        {visuals.map((visual) => (
          <VisualBlock
            key={visual.id}
            visual={visual}
            result={results[visual.id]}
            onFilter={onFilter}
            onNext={onNext}
            onPrevious={onPrevious}
            canPrevious={canPrevious[visual.id] === true}
            onConfigure={(visual) => setEditing(visual)}
          />
        ))}
      </div>
      {creating && <div className="fixed bottom-0 right-0 top-12 z-50"><VisualBuilderDrawer catalog={catalog} onClose={() => setCreating(false)} onSave={(visual) => { onAddVisual(visual); setCreating(false); }} /></div>}
      {editing && <div className="fixed bottom-0 right-0 top-12 z-50"><VisualBuilderDrawer catalog={catalog} initial={editing} onClose={() => setEditing(null)} onSave={(visual) => { onConfigure?.(visual); setEditing(null); }} /></div>}
    </div>
  );
}

function VisualBlock({ visual, result, onFilter, onNext, onPrevious, canPrevious, onConfigure }: { visual: AnalyticsVisual; result?: AnalyticsQueryResult; onFilter: AnalyticsWorkbenchProps["onFilter"]; onNext: AnalyticsWorkbenchProps["onNext"]; onPrevious?: AnalyticsWorkbenchProps["onPrevious"]; canPrevious: boolean; onConfigure?: AnalyticsWorkbenchProps["onConfigure"] }) {
  const rows = result?.rows ?? [];
  const columns = result?.columns ?? [...visual.dimensions, ...visual.metrics];
  return (
    <section aria-label={visual.title} className={`min-w-0 rounded-lg border bg-card ${visual.kind === "activity" ? "xl:col-span-2" : ""}`}>
      <header className="flex items-start justify-between border-b px-4 py-3">
        <div><h2 className="text-sm font-medium">{visual.title}</h2><p className="text-[11px] text-muted-foreground">{visual.metrics.join(" · ")} by {visual.dimensions.join(" · ")}</p></div>
        <button type="button" onClick={() => onConfigure?.(visual)} className="rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground">Configure</button>
      </header>
      {visual.presentation === "metric" ? (
        <MetricVisual visual={visual} rows={rows} />
      ) : visual.presentation === "line" ? (
        <LineVisual visual={visual} rows={rows} />
      ) : visual.presentation === "stacked" ? (
        <BarVisual visual={visual} rows={rows} onFilter={onFilter} />
      ) : visual.kind === "activity" ? (
        <div className="grid grid-cols-7 gap-1 p-4" aria-label="Activity grid">
          {rows.map((row, index) => {
            const runs = numeric(row.runs);
            return <button key={`${String(row.time)}:${index}`} type="button" title={`${String(row.time)} · ${runs} runs`} onClick={() => onFilter("time", String(row.time), "in")} className="aspect-square min-h-8 rounded border bg-primary/10 text-[10px] tabular-nums hover:bg-primary/20">{runs}</button>;
          })}
          {rows.length === 0 && <p className="col-span-7 py-6 text-center text-xs text-muted-foreground">No activity matches the filters.</p>}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs"><thead className="text-[10px] uppercase tracking-wide text-muted-foreground"><tr>{columns.map((column) => <th key={column} className="px-3 py-2 font-medium">{column.replaceAll("_", " ")}</th>)}</tr></thead>
            <tbody>{rows.map((row, index) => <tr key={index} className="border-t">{columns.map((column) => <td key={column} className="px-3 py-2 tabular-nums">{column === "debug_link" && typeof row[column] === "string" ? <a href={row[column]} className="font-medium text-primary underline-offset-2 hover:underline">Open context</a> : column === "trace" && row[column] ? <span className="font-mono text-[11px]">{formatValue(row[column])}</span> : DIMENSIONS.has(column as AnalyticsDimension) && row[column] != null ? <button type="button" aria-label={`Include ${column} ${String(row[column])}`} onClick={() => onFilter(column as AnalyticsDimension, String(row[column]), "in")} className="rounded px-1 py-0.5 text-left hover:bg-muted">{formatValue(row[column])}</button> : formatValue(row[column])}</td>)}</tr>)}</tbody>
          </table>
          {rows.length === 0 && <p className="px-4 py-8 text-center text-xs text-muted-foreground">No data matches the filters.</p>}
        </div>
      )}
      <footer className="flex items-center justify-between border-t px-3 py-2">
        <span className="text-[11px] text-muted-foreground">{rows.length} rows</span>
        <div className="flex gap-1"><button type="button" aria-label={`Previous ${visual.title} page`} disabled={!canPrevious} onClick={() => onPrevious?.(visual.id)} className="rounded border px-2 py-1 text-xs disabled:opacity-40">Previous</button><button type="button" aria-label={`Next ${visual.title} page`} disabled={!result?.next_cursor} onClick={() => result?.next_cursor && onNext(visual.id, result.next_cursor)} className="rounded border px-2 py-1 text-xs disabled:opacity-40">Next</button></div>
      </footer>
    </section>
  );
}

function MetricVisual({ visual, rows }: { visual: AnalyticsVisual; rows: Record<string, unknown>[] }) {
  const metric = visual.metrics[0];
  return (
    <div className="p-6">
      <p className="text-[10px] uppercase tracking-wide text-muted-foreground">{metric?.replaceAll("_", " ")}</p>
      <p className="mt-2 font-mono text-3xl font-semibold">{formatValue(metric ? rows[0]?.[metric] : 0)}</p>
    </div>
  );
}

function LineVisual({ visual, rows }: { visual: AnalyticsVisual; rows: Record<string, unknown>[] }) {
  const metric = visual.metrics[0];
  const values = rows.map((row) => numeric(metric ? row[metric] : 0));
  const max = Math.max(1, ...values);
  const points = values.map((value, index) => {
    const x = values.length <= 1 ? 50 : (index / (values.length - 1)) * 100;
    const y = 38 - (value / max) * 34;
    return `${x},${y}`;
  }).join(" ");
  return (
    <figure className="p-4" aria-label={`${visual.title} chart`} role="img">
      <svg viewBox="0 0 100 42" className="h-40 w-full overflow-visible text-primary" preserveAspectRatio="none">
        <path d="M0 39H100" className="stroke-border" strokeWidth="0.4" />
        <polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.4" vectorEffect="non-scaling-stroke" />
      </svg>
    </figure>
  );
}

function BarVisual({ visual, rows, onFilter }: { visual: AnalyticsVisual; rows: Record<string, unknown>[]; onFilter: AnalyticsWorkbenchProps["onFilter"] }) {
  const metric = visual.metrics[0];
  const dimension = visual.dimensions[0];
  const max = Math.max(1, ...rows.map((row) => numeric(metric ? row[metric] : 0)));
  return (
    <div className="space-y-2 p-4">
      {rows.map((row, index) => {
        const value = numeric(metric ? row[metric] : 0);
        const dimensionValue = dimension ? String(row[dimension] ?? "Unknown") : `Row ${index + 1}`;
        return (
          <button key={`${dimensionValue}:${index}`} type="button" onClick={() => dimension && onFilter(dimension, dimensionValue, "in")} className="grid w-full grid-cols-[110px_1fr_50px] items-center gap-3 text-left text-xs">
            <span className="truncate text-muted-foreground">{dimensionValue}</span>
            <span className="h-2 overflow-hidden rounded bg-muted"><span className="block h-full rounded bg-primary" style={{ width: `${(value / max) * 100}%` }} /></span>
            <span className="text-right font-mono">{formatValue(value)}</span>
          </button>
        );
      })}
    </div>
  );
}

function numeric(value: unknown): number { return typeof value === "number" ? value : Number(value) || 0; }
function formatValue(value: unknown): string { if (value == null || value === "") return "Unknown"; if (typeof value === "number") return new Intl.NumberFormat("en", { maximumFractionDigits: 2 }).format(value); return String(value); }
