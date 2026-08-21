"use client";

import type { GranularityHours } from "@/lib/types";

// Preset windows. Arbitrary from/to isn't offered — the plan scoped this to
// a small set of common ranges rather than a full date-range picker.
export const WINDOW_PRESETS = [
  { label: "24h", hours: 24 },
  { label: "7d", hours: 24 * 7 },
  { label: "30d", hours: 24 * 30 },
  { label: "90d", hours: 24 * 90 },
] as const;

export type WindowHours = (typeof WINDOW_PRESETS)[number]["hours"];

export const DEFAULT_WINDOW_HOURS: WindowHours = 24 * 7;
export const DEFAULT_GRANULARITY_HOURS: GranularityHours = 6;

// Which granularities each window offers, sized to keep the bar count
// scannable (roughly 4-30 bars) while staying inside the allowlist the
// route handler enforces (GRANULARITY_HOURS in lib/types.ts).
const GRANULARITY_OPTIONS: Record<WindowHours, readonly GranularityHours[]> = {
  24: [1, 3, 6],
  [24 * 7]: [6, 24],
  [24 * 30]: [24],
  [24 * 90]: [24, 168],
};

export function granularityOptionsFor(windowHours: WindowHours): readonly GranularityHours[] {
  return GRANULARITY_OPTIONS[windowHours];
}

function granularityLabel(hours: GranularityHours): string {
  return hours === 168 ? "1w" : `${hours}h`;
}

// Local pill control, ported from packages/views/runtimes/components/usage-section.tsx's
// Segmented (admin doesn't depend on packages/views).
function Segmented<T extends string | number>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
}) {
  return (
    <div className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-caption font-medium transition-colors ${
            o.value === value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function WindowToolbar({
  windowHours,
  onWindowChange,
  granularityHours,
  onGranularityChange,
}: {
  windowHours: WindowHours;
  onWindowChange: (hours: WindowHours) => void;
  granularityHours: GranularityHours;
  onGranularityChange: (hours: GranularityHours) => void;
}) {
  const granularityChoices = granularityOptionsFor(windowHours);
  return (
    <div className="flex flex-wrap items-center gap-4">
      <div className="flex items-center gap-2">
        <span className="text-caption uppercase tracking-wider text-muted-foreground">Window</span>
        <Segmented
          value={windowHours}
          onChange={onWindowChange}
          options={WINDOW_PRESETS.map((p) => ({ label: p.label, value: p.hours }))}
        />
      </div>
      <div className="flex items-center gap-2">
        <span className="text-caption uppercase tracking-wider text-muted-foreground">Granularity</span>
        <Segmented
          value={granularityHours}
          onChange={onGranularityChange}
          options={granularityChoices.map((h) => ({ label: granularityLabel(h), value: h }))}
        />
      </div>
    </div>
  );
}
