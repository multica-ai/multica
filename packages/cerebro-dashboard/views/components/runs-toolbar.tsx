"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import type { AnalyticsDimension, AnalyticsOperator } from "@multica/cerebro-usage";
import type { AnalyticsFilter } from "../../core/analytics";
import type { TimeRange } from "../../core/types";

const FILTER_DIMENSIONS: AnalyticsDimension[] = [
  "person",
  "agent",
  "project",
  "runtime",
  "source",
  "provider",
  "model",
  "skill",
  "status",
  "cost_kind",
  "quality_type",
  "quality_category",
];

export function RunsToolbar({
  range,
  onRangeChange,
  filters,
  onAddFilter,
  onRemoveFilter,
  onClear,
  onCustomize,
  onNewVisual,
}: {
  range: TimeRange;
  onRangeChange: (range: TimeRange) => void;
  filters: AnalyticsFilter[];
  onAddFilter: (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in") => void;
  onRemoveFilter: (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => void;
  onClear: () => void;
  onCustomize: () => void;
  onNewVisual: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [dimension, setDimension] = useState<AnalyticsDimension>("person");
  const [operator, setOperator] = useState<"in" | "not_in">("in");
  const [value, setValue] = useState("");
  const chips = filters.flatMap((filter) => filter.values.map((filterValue) => ({ ...filter, value: filterValue })));

  return (
    <div className="relative border-b pb-3">
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label="Time range" value={range} onChange={(event) => onRangeChange(event.target.value as TimeRange)} className="h-8 rounded-md border bg-muted/30 px-3 text-xs">
          <option value="24h">Last 24 hours</option>
          <option value="7d">Last 7 days</option>
          <option value="30d">Last 30 days</option>
        </select>
        <button type="button" aria-expanded={adding} onClick={() => setAdding((open) => !open)} className="flex h-8 items-center gap-1.5 rounded-md bg-[#6557d8] px-3 text-xs font-semibold text-white hover:bg-[#5749c7] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8]">
          <Plus className="size-3.5" /> Add filter
        </button>
        <button type="button" onClick={onCustomize} className="h-8 rounded-md border bg-background px-3 text-xs font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">Customize layout</button>
        <button type="button" onClick={onNewVisual} className="flex h-8 items-center gap-1.5 rounded-md border bg-background px-3 text-xs font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><Plus className="size-3.5" /> New visual</button>
        {chips.map((chip) => (
          <button
            key={`${chip.dimension}:${chip.operator}:${chip.value}`}
            type="button"
            aria-label={`${titleCase(chip.dimension)}: ${chip.value} ×`}
            onClick={() => onRemoveFilter(chip.dimension, chip.value, chip.operator)}
            className={`h-8 rounded-md border px-2.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8] ${chip.operator === "not_in" ? "border-amber-400 bg-amber-50 text-amber-800" : "border-[rgba(101,87,216,0.35)] bg-[rgba(101,87,216,0.10)] text-[#4e43ad]"}`}
          >
            {titleCase(chip.dimension)}: {chip.value} ×
          </button>
        ))}
        {chips.length > 0 && <button type="button" onClick={onClear} className="ml-auto h-8 px-2 text-xs text-muted-foreground hover:text-foreground">Clear all</button>}
      </div>

      {adding && (
        <form
          aria-label="Add Dashboard filter"
          className="absolute left-0 top-10 z-30 grid w-[min(520px,calc(100vw-3rem))] grid-cols-1 gap-2 rounded-lg border bg-popover p-3 shadow-lg sm:grid-cols-[1fr_110px_1fr_auto]"
          onSubmit={(event) => {
            event.preventDefault();
            const normalized = value.trim();
            if (!normalized) return;
            onAddFilter(dimension, normalized, operator);
            setValue("");
            setAdding(false);
          }}
        >
          <select aria-label="Filter dimension" value={dimension} onChange={(event) => setDimension(event.target.value as AnalyticsDimension)} className="h-9 rounded-md border bg-background px-2 text-xs">
            {FILTER_DIMENSIONS.map((option) => <option key={option} value={option}>{titleCase(option)}</option>)}
          </select>
          <select aria-label="Filter operator" value={operator} onChange={(event) => setOperator(event.target.value as "in" | "not_in")} className="h-9 rounded-md border bg-background px-2 text-xs">
            <option value="in">is</option>
            <option value="not_in">is not</option>
          </select>
          <input autoFocus aria-label="Filter value" value={value} onChange={(event) => setValue(event.target.value)} placeholder="Type a value" className="h-9 rounded-md border bg-background px-3 text-xs" />
          <button type="submit" className="h-9 rounded-md bg-[#6557d8] px-3 text-xs font-semibold text-white">Apply filter</button>
        </form>
      )}
    </div>
  );
}

function titleCase(value: string): string {
  return value.replaceAll("_", " ").replace(/^./, (character) => character.toUpperCase());
}
