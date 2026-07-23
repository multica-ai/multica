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
  const [selectedMetrics, setSelectedMetrics] = useState<AnalyticsMetric[]>(initial?.metrics.length ? initial.metrics : [preferred(metrics, "runs")]);
  const [dimension, setDimension] = useState<AnalyticsDimension>(initial?.dimensions[0] ?? preferred(dimensions, "project"));
  const [breakdown, setBreakdown] = useState<AnalyticsDimension>(initial?.dimensions[1] ?? preferred(dimensions, "person"));
  const [grain, setGrain] = useState<AnalyticsGrain>(initial?.grain === "none" ? preferred(grains, "day") : initial?.grain ?? preferred(grains, "day"));
  const visualMetrics = presentation === "table" ? selectedMetrics : selectedMetrics.slice(0, 1);
  const title = `${titleCase(dimension)} ${visualMetrics.map(label).join(" + ")} by ${label(breakdown)}`;

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
            metrics: visualMetrics,
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
                className={`flex min-h-9 w-full items-center rounded-md border px-3 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8] ${presentation === option.id ? "border-[#c9c7c3] bg-[#f0efed] text-[#242228]" : "bg-[#f7f7f6] hover:border-[#9e9b96]"}`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </BuilderSection>

        <BuilderSection title="2. Data">
          <div className="space-y-2">
            {visualMetrics.map((metric, index) => (
              <BuilderSelect
                key={index}
                label={presentation === "table" ? `Metric ${index + 1}` : "Metric"}
                value={metric}
                values={metrics}
                onChange={(value) => setSelectedMetrics((current) => current.map((item, itemIndex) => itemIndex === index ? value as AnalyticsMetric : item))}
              />
            ))}
            {presentation === "table" && selectedMetrics.length < metrics.length && (
              <button type="button" onClick={() => setSelectedMetrics((current) => [...current, metrics.find((metric) => !current.includes(metric)) ?? metrics[0]!])} className="w-full rounded-md border px-3 py-2 text-xs font-medium hover:bg-muted">
                Add metric
              </button>
            )}
            <BuilderSelect label="Dimension" value={dimension} values={dimensions} onChange={(value) => setDimension(value as AnalyticsDimension)} />
            <BuilderSelect label="Breakdown" value={breakdown} values={dimensions} onChange={(value) => setBreakdown(value as AnalyticsDimension)} />
            <BuilderSelect label="Grain" value={grain} values={grains} onChange={(value) => setGrain(value as AnalyticsGrain)} />
          </div>
        </BuilderSection>

        <BuilderSection title="3. Preview">
          <div role="region" aria-label="Visual preview" className="min-h-28 rounded-md border border-primary/30 p-3">
            <p className="mb-3 truncate text-[10px] font-semibold text-muted-foreground">{title}</p>
            <VisualPreview presentation={presentation} metrics={visualMetrics} dimension={dimension} breakdown={breakdown} />
          </div>
        </BuilderSection>

        <button type="submit" className="mt-4 min-h-9 rounded-md bg-[#6557d8] px-4 text-xs font-semibold text-white hover:bg-[#5749c7] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8]">
          {initial ? "Save visual" : "Add visual to Dashboard"}
        </button>
      </form>
    </aside>
  );
}

function VisualPreview({ presentation, metrics, dimension, breakdown }: { presentation: AnalyticsVisualPresentation; metrics: AnalyticsMetric[]; dimension: AnalyticsDimension; breakdown: AnalyticsDimension }) {
  if (presentation === "metric") {
    return <div><p className="text-[10px] uppercase text-muted-foreground">{titleCase(metrics[0] ?? "metric")}</p><p className="font-mono text-2xl font-semibold">128</p></div>;
  }
  if (presentation === "table") {
    return (
      <table className="w-full text-[10px]">
        <thead><tr className="border-b text-left text-muted-foreground"><th className="pb-1 font-medium">{titleCase(dimension)}</th>{metrics.map((metric) => <th key={metric} className="pb-1 text-right font-medium">{titleCase(metric)}</th>)}</tr></thead>
        <tbody>{["A", "B", "C"].map((row, index) => <tr key={row}><td className="py-1">{titleCase(breakdown)} {row}</td>{metrics.map((metric, metricIndex) => <td key={metric} className="py-1 text-right font-mono">{(index + 1) * (metricIndex + 2) * 8}</td>)}</tr>)}</tbody>
      </table>
    );
  }
  if (presentation === "activity") {
    return <div className="grid grid-cols-8 gap-1">{Array.from({ length: 24 }, (_, index) => <span key={index} className={`h-3 rounded-sm ${index % 4 === 0 ? "bg-primary" : index % 3 === 0 ? "bg-primary/60" : "bg-primary/20"}`} />)}</div>;
  }
  if (presentation === "stacked") {
    return <div className="space-y-2">{[80, 55, 70].map((width) => <div key={width} className="flex h-3 overflow-hidden rounded bg-muted"><span className="bg-primary" style={{ width: `${width * 0.65}%` }} /><span className="bg-primary/45" style={{ width: `${width * 0.35}%` }} /></div>)}</div>;
  }
  return (
    <svg viewBox="0 0 100 34" className="h-20 w-full overflow-visible text-primary" aria-label={`${titleCase(metrics[0] ?? "metric")} preview chart`}>
      <polyline points="0,29 25,18 50,23 75,7 100,12" fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      {[29, 18, 23, 7, 12].map((y, index) => <circle key={index} cx={index * 25} cy={y} r="1.8" fill="currentColor" />)}
    </svg>
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
      <select aria-label={selectLabel} value={value} onChange={(event) => onChange(event.target.value)} className="min-h-7 appearance-none border-0 bg-transparent px-0 text-right font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8]">
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
