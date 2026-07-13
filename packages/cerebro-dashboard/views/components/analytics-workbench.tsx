"use client";

import { useState } from "react";
import type { AnalyticsCatalog, AnalyticsDimension, AnalyticsQueryResult } from "@multica/cerebro-usage";
import type { AnalyticsFilter, AnalyticsVisual } from "../../core/analytics";

type Results = Record<string, AnalyticsQueryResult | undefined>;

interface AnalyticsWorkbenchProps {
  visuals: AnalyticsVisual[];
  results: Results;
  filters: AnalyticsFilter[];
  onFilter: (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in") => void;
  onNext: (visualId: string, cursor: string) => void;
  onPrevious?: (visualId: string) => void;
  canPrevious?: Record<string, boolean>;
  onAddVisual: (visual: AnalyticsVisual) => void;
  onConfigure?: (visual: AnalyticsVisual) => void;
  catalog?: AnalyticsCatalog;
}

const DIMENSIONS = new Set<AnalyticsDimension>([
  "time", "person", "agent", "project", "source", "provider", "model", "skill", "status", "quality_type", "quality_category", "context", "run", "source_id", "reference", "reference_label",
]);

export function AnalyticsWorkbench({ visuals, results, filters, onFilter, onNext, onPrevious, canPrevious = {}, onAddVisual, onConfigure, catalog }: AnalyticsWorkbenchProps) {
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<AnalyticsVisual | null>(null);
  return (
    <div className="space-y-4" aria-label="Analytics workspace">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-3">
        <div className="flex flex-wrap gap-1.5" aria-label="Active filters">
          {filters.length === 0 ? <span className="text-xs text-muted-foreground">All workspace activity</span> : filters.flatMap((filter) => filter.values.map((value) => (
            <button key={`${filter.dimension}:${filter.operator}:${value}`} type="button" onClick={() => onFilter(filter.dimension, value, filter.operator)} className="rounded border bg-muted/40 px-2 py-1 text-xs">
              {filter.dimension} {filter.operator === "not_in" ? "≠" : "="} {value} ×
            </button>
          )))}
        </div>
        <button type="button" aria-label="New visual" onClick={() => setCreating(true)} className="rounded-md border bg-background px-3 py-1.5 text-xs font-medium hover:bg-muted">New visual</button>
      </div>

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
      {creating && <VisualDialog catalog={catalog} onClose={() => setCreating(false)} onSave={(visual) => { onAddVisual(visual); setCreating(false); }} />}
      {editing && <VisualDialog catalog={catalog} initial={editing} onClose={() => setEditing(null)} onSave={(visual) => { onConfigure?.(visual); setEditing(null); }} />}
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
      {visual.kind === "activity" ? (
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

function VisualDialog({ initial, catalog, onClose, onSave }: { initial?: AnalyticsVisual; catalog?: AnalyticsCatalog; onClose: () => void; onSave: (visual: AnalyticsVisual) => void }) {
  const [title, setTitle] = useState(initial?.title ?? "Untitled visual");
  const [kind, setKind] = useState<AnalyticsVisual["kind"]>(initial?.kind ?? "table");
  const [metric, setMetric] = useState(initial?.metrics[0] ?? catalog?.metrics[0] ?? "runs");
  const [dimension, setDimension] = useState(initial?.dimensions[0] ?? catalog?.dimensions[0] ?? "time");
  const [grain, setGrain] = useState(initial?.grain ?? "day");
  const metrics = catalog?.metrics.length ? catalog.metrics : ["runs", "cost_cents", "saved_cents", "quality_pass_rate"] as const;
  const dimensions = catalog?.dimensions.length ? catalog.dimensions : ["time", "person", "project", "source", "provider", "model", "skill", "status", "quality_category", "context", "run", "reference_label", "debug_link", "trace"] as const;
  const grains = catalog?.grains.length ? catalog.grains : ["hour", "day", "week", "month"] as const;
  const submitLabel = initial ? "Save visual" : "Create visual";
  return <div role="dialog" aria-label={initial ? "Configure visual" : "New visual"} className="fixed inset-0 z-50 grid place-items-center bg-black/20 p-4"><form onSubmit={(event) => { event.preventDefault(); onSave({ id: initial?.id ?? `custom-${Date.now()}`, title, kind, metrics: [metric], dimensions: [dimension], grain: dimension === "time" ? grain : "none", limit: initial?.limit ?? 12 }); }} className="w-full max-w-md space-y-4 rounded-lg border bg-background p-5 shadow-lg"><div><h2 className="font-medium">{initial ? "Configure visual" : "New visual"}</h2><p className="text-xs text-muted-foreground">Build from the same metrics and filters as every Dashboard block.</p></div><label className="block text-xs font-medium">Visual title<input aria-label="Visual title" value={title} onChange={(event) => setTitle(event.target.value)} className="mt-1 w-full rounded border bg-muted/30 px-3 py-2 font-normal" /></label><div className="grid grid-cols-2 gap-3"><Select label="Visual type" value={kind} values={["table", "bars", "activity"]} onChange={(value) => setKind(value as AnalyticsVisual["kind"])} /><Select label="Metric" value={metric} values={metrics} onChange={(value) => setMetric(value as typeof metric)} /><Select label="Dimension" value={dimension} values={dimensions} onChange={(value) => setDimension(value as typeof dimension)} /><Select label="Grain" value={grain} values={grains} onChange={(value) => setGrain(value as typeof grain)} /></div><div className="flex justify-end gap-2"><button type="button" onClick={onClose} className="rounded px-3 py-2 text-xs">Cancel</button><button type="submit" className="rounded bg-primary px-3 py-2 text-xs text-primary-foreground">{submitLabel}</button></div></form></div>;
}

function Select({ label, value, values, onChange }: { label: string; value: string; values: readonly string[]; onChange: (value: string) => void }) { return <label className="block text-xs font-medium">{label}<select aria-label={label} value={value} onChange={(event) => onChange(event.target.value)} className="mt-1 w-full rounded border bg-muted/30 px-3 py-2 font-normal">{values.map((item) => <option key={item} value={item}>{item.replaceAll("_", " ")}</option>)}</select></label>; }

function numeric(value: unknown): number { return typeof value === "number" ? value : Number(value) || 0; }
function formatValue(value: unknown): string { if (value == null || value === "") return "Unknown"; if (typeof value === "number") return new Intl.NumberFormat("en", { maximumFractionDigits: 2 }).format(value); return String(value); }
