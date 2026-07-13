"use client";

import { useState } from "react";
import { X } from "lucide-react";
import type {
  AnalyticsCatalog,
  AnalyticsDimension,
  AnalyticsGrain,
  AnalyticsMetric,
} from "@multica/cerebro-usage";
import {
  presentationToVisualKind,
  type AnalyticsVisual,
  type AnalyticsVisualPresentation,
} from "../../core/analytics";

const PRESENTATIONS: { id: AnalyticsVisualPresentation; label: string }[] = [
  { id: "line", label: "Line chart" },
  { id: "activity", label: "Activity grid" },
  { id: "stacked", label: "Stacked bars" },
  { id: "table", label: "Table" },
  { id: "metric", label: "Single metric" },
];

const FALLBACK_METRICS: AnalyticsMetric[] = ["runs", "cost_cents", "saved_cents"];
const FALLBACK_DIMENSIONS: AnalyticsDimension[] = ["project", "person", "time"];
const FALLBACK_GRAINS: AnalyticsGrain[] = ["day", "week", "month"];

export function VisualBuilderDrawer({
  catalog,
  initial,
  onClose,
  onSave,
}: {
  catalog?: AnalyticsCatalog;
  initial?: AnalyticsVisual;
  onClose: () => void;
  onSave: (visual: AnalyticsVisual) => void;
}) {
  const metrics = catalog?.metrics.length ? catalog.metrics : FALLBACK_METRICS;
  const dimensions = catalog?.dimensions.length ? catalog.dimensions : FALLBACK_DIMENSIONS;
  const catalogGrains = catalog?.grains.filter((grain) => grain !== "none") ?? [];
  const grains = catalogGrains.length ? catalogGrains : FALLBACK_GRAINS;
  const [presentation, setPresentation] = useState<AnalyticsVisualPresentation>(initial?.presentation ?? "line");
  const [metric, setMetric] = useState<AnalyticsMetric>(initial?.metrics[0] ?? preferred(metrics, "runs"));
  const [dimension, setDimension] = useState<AnalyticsDimension>(initial?.dimensions[0] ?? preferred(dimensions, "project"));
  const [breakdown, setBreakdown] = useState<AnalyticsDimension>(initial?.dimensions[1] ?? preferred(dimensions, "person"));
  const [grain, setGrain] = useState<AnalyticsGrain>(initial?.grain === "none" ? preferred(grains, "day") : initial?.grain ?? preferred(grains, "day"));
  const title = `${titleCase(dimension)} ${label(metric)} by ${label(breakdown)}`;

  return (
    <aside aria-label={initial ? "Configure visual" : "New visual"} className="flex h-full w-[350px] shrink-0 flex-col overflow-y-auto border-l bg-card">
      <header className="flex items-start justify-between border-b px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold">{initial ? "Configure visual" : "New visual"}</h2>
          <p className="font-mono text-[10px] text-muted-foreground">Uses current Dashboard filters</p>
        </div>
        <button type="button" aria-label={initial ? "Close configure visual" : "Close new visual"} onClick={onClose} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <X className="size-4" />
        </button>
      </header>

      <form
        className="flex flex-1 flex-col p-4"
        onSubmit={(event) => {
          event.preventDefault();
          onSave({
            id: initial?.id ?? `custom-${Date.now()}`,
            title,
            kind: presentationToVisualKind(presentation),
            presentation,
            metrics: [metric],
            dimensions: dimension === breakdown ? [dimension] : [dimension, breakdown],
            grain,
            limit: initial?.limit ?? 12,
          });
        }}
      >
        <BuilderSection title="1. Visualization">
          <div className="space-y-1.5">
            {PRESENTATIONS.map((option) => (
              <button
                key={option.id}
                type="button"
                aria-pressed={presentation === option.id}
                onClick={() => setPresentation(option.id)}
                className={`flex min-h-9 w-full items-center rounded-md border px-3 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${presentation === option.id ? "border-primary bg-primary/10 text-primary" : "bg-muted/30 hover:border-primary/50"}`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </BuilderSection>

        <BuilderSection title="2. Data">
          <div className="space-y-2">
            <BuilderSelect label="Metric" value={metric} values={metrics} onChange={(value) => setMetric(value as AnalyticsMetric)} />
            <BuilderSelect label="Dimension" value={dimension} values={dimensions} onChange={(value) => setDimension(value as AnalyticsDimension)} />
            <BuilderSelect label="Breakdown" value={breakdown} values={dimensions} onChange={(value) => setBreakdown(value as AnalyticsDimension)} />
            <BuilderSelect label="Grain" value={grain} values={grains} onChange={(value) => setGrain(value as AnalyticsGrain)} />
          </div>
        </BuilderSection>

        <BuilderSection title="3. Preview">
          <div className="grid min-h-24 place-items-center rounded-md border border-dashed border-primary/30 px-4 text-center">
            <div>
              <p className="text-xs text-muted-foreground">{title}</p>
              <p className="mt-3 text-[10px] text-muted-foreground">Preview updates as fields change</p>
            </div>
          </div>
        </BuilderSection>

        <button type="submit" className="mt-4 min-h-9 rounded-md bg-primary px-4 text-xs font-semibold text-primary-foreground hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {initial ? "Save visual" : "Add visual to Dashboard"}
        </button>
      </form>
    </aside>
  );
}

function BuilderSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b py-4 first:pt-0">
      <h3 className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function BuilderSelect({ label: selectLabel, value, values, onChange }: { label: string; value: string; values: readonly string[]; onChange: (value: string) => void }) {
  return (
    <label className="grid grid-cols-[90px_1fr] items-center gap-3 text-xs">
      <span className="text-muted-foreground">{selectLabel}</span>
      <select aria-label={selectLabel} value={value} onChange={(event) => onChange(event.target.value)} className="min-h-9 rounded-md border bg-muted/30 px-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {values.map((valueOption) => <option key={valueOption} value={valueOption}>{titleCase(valueOption)}</option>)}
      </select>
    </label>
  );
}

function preferred<T extends string>(values: readonly T[], preferredValue: T): T {
  return values.includes(preferredValue) ? preferredValue : values[0]!;
}

function titleCase(value: string): string {
  return value.replaceAll("_", " ").replace(/^./, (character) => character.toUpperCase());
}

function label(value: string): string {
  return value.replaceAll("_", " ");
}
