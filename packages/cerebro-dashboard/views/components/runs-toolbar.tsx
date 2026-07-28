"use client";

import { useMemo, useState } from "react";
import { MoreHorizontal, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { analyticsQueryOptions, type AnalyticsDimension, type AnalyticsOperator } from "@multica/cerebro-usage";
import type { AnalyticsFilter } from "../../core/analytics";
import { dimensionLabel, ENUMERABLE_DIMENSIONS, operatorLabel, valueLabel } from "../../core/dimension-labels";
import type { TimeRange } from "../../core/types";

const FILTER_DIMENSIONS: AnalyticsDimension[] = [
  "person",
  "agent",
  "project",
  "issue",
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

type AddOperator = Extract<AnalyticsOperator, "in" | "not_in" | "contains" | "not_contains">;

export function RunsToolbar({
  workspaceId,
  range,
  onRangeChange,
  filters,
  onAddFilter,
  onRemoveFilter,
  onClear,
  onCustomize,
  onNewVisual,
}: {
  workspaceId: string;
  range: TimeRange;
  onRangeChange: (range: TimeRange) => void;
  filters: AnalyticsFilter[];
  onAddFilter: (dimension: AnalyticsDimension, value: string, operator: AddOperator) => void;
  onRemoveFilter: (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => void;
  onClear: () => void;
  onCustomize: () => void;
  onNewVisual: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [dimension, setDimension] = useState<AnalyticsDimension>("person");
  const [operator, setOperator] = useState<AddOperator>("in");
  const [value, setValue] = useState("");
  const chips = filters.flatMap((filter) => filter.values.map((filterValue) => ({ ...filter, value: filterValue })));

  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);
  const usePicker = (operator === "in" || operator === "not_in") && ENUMERABLE_DIMENSIONS.includes(dimension);
  const optionsQuery = useQuery({
    ...analyticsQueryOptions(workspaceId, {
      population: "all",
      metrics: ["runs"],
      dimensions: [dimension],
      grain: "none",
      page: { limit: 200 },
      timezone,
    }),
    enabled: workspaceId.length > 0 && adding && usePicker,
  });
  const options = useMemo(() => {
    const rows = optionsQuery.data?.rows ?? [];
    const seen = new Map<string, number>();
    for (const row of rows) {
      const raw = row[dimension];
      const key = raw == null ? "" : String(raw);
      const runs = typeof row.runs === "number" ? row.runs : Number(row.runs) || 0;
      seen.set(key, (seen.get(key) ?? 0) + runs);
    }
    return [...seen.entries()].sort((a, b) => b[1] - a[1]);
  }, [optionsQuery.data, dimension]);

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    const normalized = usePicker ? value : value.trim();
    if (normalized === "" && (!usePicker || !options.some(([key]) => key === ""))) return;
    onAddFilter(dimension, normalized, operator);
    setValue("");
    setAdding(false);
  };

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
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button type="button" aria-label="Dashboard actions" className="grid size-8 place-items-center rounded-md border bg-background hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <MoreHorizontal className="size-4" />
              </button>
            }
          />
          <DropdownMenuContent align="start">
            <DropdownMenuItem onClick={onCustomize}>Customize layout</DropdownMenuItem>
            <DropdownMenuItem onClick={onNewVisual}>New visual</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {chips.map((chip) => (
          <button
            key={`${chip.dimension}:${chip.operator}:${chip.value}`}
            type="button"
            aria-label={`${dimensionLabel(chip.dimension)} ${operatorLabel(chip.operator)} ${chipValue(chip.dimension, chip.operator, chip.value)} ×`}
            onClick={() => onRemoveFilter(chip.dimension, chip.value, chip.operator)}
            className={`h-8 rounded-md border px-2.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#6557d8] ${chip.operator === "not_in" || chip.operator === "not_contains" ? "border-amber-400 bg-amber-50 text-amber-800" : "border-[rgba(101,87,216,0.35)] bg-[rgba(101,87,216,0.10)] text-[#4e43ad]"}`}
          >
            {dimensionLabel(chip.dimension)} {operatorLabel(chip.operator)} {chipValue(chip.dimension, chip.operator, chip.value)} ×
          </button>
        ))}
        {chips.length > 0 && <button type="button" onClick={onClear} className="ml-auto h-8 px-2 text-xs text-muted-foreground hover:text-foreground">Clear all</button>}
      </div>

      {adding && (
        <form
          aria-label="Add Dashboard filter"
          className="absolute left-0 top-10 z-30 grid w-[min(560px,calc(100vw-3rem))] grid-cols-1 gap-2 rounded-lg border bg-popover p-3 shadow-lg sm:grid-cols-[1fr_150px_1fr_auto]"
          onSubmit={submit}
        >
          <select aria-label="Filter dimension" value={dimension} onChange={(event) => { setDimension(event.target.value as AnalyticsDimension); setValue(""); }} className="h-9 rounded-md border bg-background px-2 text-xs">
            {FILTER_DIMENSIONS.map((option) => <option key={option} value={option}>{dimensionLabel(option)}</option>)}
          </select>
          <select aria-label="Filter operator" value={operator} onChange={(event) => { setOperator(event.target.value as AddOperator); setValue(""); }} className="h-9 rounded-md border bg-background px-2 text-xs">
            <option value="in">is</option>
            <option value="not_in">is not</option>
            <option value="contains">contains</option>
            <option value="not_contains">does not contain</option>
          </select>
          {usePicker ? (
            <select aria-label="Filter value" value={value} onChange={(event) => setValue(event.target.value)} className="h-9 rounded-md border bg-background px-2 text-xs">
              <option value="" disabled={!options.some(([key]) => key === "")}>
                {optionsQuery.isLoading ? "Loading values…" : options.some(([key]) => key === "") ? valueLabel(dimension, "") : "Pick a value"}
              </option>
              {options.filter(([key]) => key !== "").map(([key, runs]) => (
                <option key={key} value={key}>{valueLabel(dimension, key)} · {runs}</option>
              ))}
            </select>
          ) : (
            <input autoFocus aria-label="Filter value" value={value} onChange={(event) => setValue(event.target.value)} placeholder="Type text to match" className="h-9 rounded-md border bg-background px-3 text-xs" />
          )}
          <button type="submit" className="h-9 rounded-md bg-[#6557d8] px-3 text-xs font-semibold text-white">Apply filter</button>
        </form>
      )}
    </div>
  );
}

function chipValue(dimension: AnalyticsDimension, operator: AnalyticsOperator, value: string): string {
  if (operator === "contains" || operator === "not_contains") return `“${value}”`;
  if (dimension === "time") {
    const date = new Date(value);
    if (!Number.isNaN(date.getTime())) return date.toLocaleString("en", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
  }
  return valueLabel(dimension, value);
}
