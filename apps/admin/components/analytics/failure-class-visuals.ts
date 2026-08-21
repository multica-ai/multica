import { FAILURE_CLASSES, type FailureClass } from "@multica/core/dashboard";
import type { ChartConfig } from "@multica/ui/components/ui/chart";

// Local port of packages/views/runtimes/components/charts/failure-class-visuals.ts
// minus i18n (admin has no i18n setup and is English-only) — same ramp so an
// operator sees a consistent error-class palette between this page and the
// per-workspace Usage → Errors tab.
export type FailureClassCounts = Record<FailureClass, number>;

export const FAILURE_CLASS_COLOR: Record<FailureClass, string> = {
  auth: "var(--destructive)",
  rate_limit: "color-mix(in oklch, var(--destructive) 86%, var(--card))",
  timeout: "color-mix(in oklch, var(--destructive) 72%, var(--card))",
  provider: "color-mix(in oklch, var(--destructive) 60%, var(--card))",
  runtime: "color-mix(in oklch, var(--destructive) 48%, var(--card))",
  agent: "color-mix(in oklch, var(--destructive) 38%, var(--card))",
  other: "color-mix(in oklch, var(--destructive) 30%, var(--card))",
};

const FAILURE_CLASS_LABEL: Record<FailureClass, string> = {
  auth: "Auth",
  rate_limit: "Rate limit",
  timeout: "Timeout",
  provider: "Provider",
  runtime: "Runtime",
  agent: "Agent",
  other: "Other",
};

export const failureClassChartConfig: ChartConfig = Object.fromEntries(
  FAILURE_CLASSES.map((c) => [c, { label: FAILURE_CLASS_LABEL[c], color: FAILURE_CLASS_COLOR[c] }]),
) as ChartConfig;

/** Which classes to draw, in canonical order — skips permanently-zero series. */
export function activeFailureClasses(
  data: readonly Partial<Record<FailureClass, number>>[],
): FailureClass[] {
  return FAILURE_CLASSES.filter((c) => data.some((d) => (d[c] ?? 0) > 0));
}

/** Recharts passes the dataKey through as the tooltip item's `name` — look up
 * the translated label from the same config the legend/bars use. */
export function labelOf(config: ChartConfig, name: string | number | undefined): string {
  const key = String(name ?? "");
  const label = config[key]?.label;
  return typeof label === "string" ? label : key;
}
