import type {
  AnalyticsDimension,
  AnalyticsGrain,
  AnalyticsMetric,
  AnalyticsOperator,
  AnalyticsQuery,
} from "@multica/cerebro-usage";

export type AnalyticsVisualKind = "activity" | "table" | "bars";

export interface AnalyticsVisual {
  id: string;
  title: string;
  kind: AnalyticsVisualKind;
  metrics: AnalyticsMetric[];
  dimensions: AnalyticsDimension[];
  grain: AnalyticsGrain;
  limit: number;
}

export type AnalyticsFilter = {
  dimension: AnalyticsDimension;
  operator: Extract<AnalyticsOperator, "in" | "not_in">;
  values: string[];
};

const FILTER_DIMENSIONS: AnalyticsDimension[] = ["person", "agent", "project", "source", "provider", "model", "skill", "status", "quality_type", "quality_category", "context", "run", "source_id", "reference", "reference_label", "trace"];

export const DEFAULT_ANALYTICS_VISUALS: AnalyticsVisual[] = [
  { id: "activity", title: "Activity", kind: "activity", metrics: ["runs", "cost_cents", "saved_cents"], dimensions: ["time"], grain: "day", limit: 42 },
  { id: "people", title: "People and projects", kind: "table", metrics: ["runs", "cost_cents", "duration_seconds"], dimensions: ["person", "project"], grain: "none", limit: 12 },
  { id: "models", title: "Provider, model and skill", kind: "bars", metrics: ["runs", "cost_cents", "skill_invocations"], dimensions: ["provider", "model", "skill"], grain: "none", limit: 12 },
  { id: "quality", title: "Quality and context", kind: "table", metrics: ["runs", "quality_pass_rate"], dimensions: ["quality_category", "quality_type", "source", "context"], grain: "none", limit: 12 },
  { id: "runs", title: "Runs and debug links", kind: "table", metrics: ["cost_cents", "duration_seconds"], dimensions: ["run", "time", "person", "source", "reference_label", "provider", "model", "skill", "status", "debug_link", "trace"], grain: "none", limit: 20 },
];

export function buildVisualQuery(
  visual: AnalyticsVisual,
  filters: AnalyticsFilter[],
  timezone: string,
  cursor?: string,
): AnalyticsQuery {
  const query: AnalyticsQuery = {
    population: "all",
    metrics: visual.metrics,
    dimensions: visual.dimensions,
    grain: visual.grain,
    filters,
    page: { limit: visual.limit, ...(cursor ? { cursor } : {}) },
    timezone,
  };
  if (visual.dimensions.includes("time")) {
    query.sort = [{ field: "time", direction: "desc" }];
  }
  return query;
}

export function toggleAnalyticsFilter(
  filters: AnalyticsFilter[],
  dimension: AnalyticsDimension,
  value: string,
  operator: AnalyticsFilter["operator"],
): AnalyticsFilter[] {
  const opposite = operator === "in" ? "not_in" : "in";
  const withoutOpposite = filters
    .map((filter) =>
      filter.dimension === dimension && filter.operator === opposite
        ? { ...filter, values: filter.values.filter((item) => item !== value) }
        : filter,
    )
    .filter((filter) => filter.values.length > 0);
  const existing = withoutOpposite.find(
    (filter) => filter.dimension === dimension && filter.operator === operator,
  );
  if (!existing) {
    return [...withoutOpposite, { dimension, operator, values: [value] }];
  }
  const values = existing.values.includes(value)
    ? existing.values.filter((item) => item !== value)
    : [...existing.values, value].sort();
  return withoutOpposite
    .map((filter) => (filter === existing ? { ...filter, values } : filter))
    .filter((filter) => filter.values.length > 0);
}

export function filtersFromSearchParams(params: URLSearchParams): AnalyticsFilter[] {
  const filters: AnalyticsFilter[] = [];
  for (const dimension of FILTER_DIMENSIONS) {
    const include = [...new Set(params.getAll(dimension))].sort();
    const exclude = [...new Set(params.getAll(`exclude.${dimension}`))].sort();
    if (include.length) filters.push({ dimension, operator: "in", values: include });
    if (exclude.length) filters.push({ dimension, operator: "not_in", values: exclude });
  }
  return filters;
}

export function filtersToSearchParams(filters: AnalyticsFilter[], current = new URLSearchParams()): URLSearchParams {
  const params = new URLSearchParams(current);
  for (const dimension of FILTER_DIMENSIONS) {
    params.delete(dimension);
    params.delete(`exclude.${dimension}`);
  }
  for (const filter of filters) {
    const key = filter.operator === "not_in" ? `exclude.${filter.dimension}` : filter.dimension;
    filter.values.forEach((value) => params.append(key, value));
  }
  return params;
}
