export const USAGE_FILTER_DIMENSIONS = [
  "project",
  "group",
  "member",
  "agent",
  "runtime",
  "model",
  "provider",
  "trigger",
  "status",
  "cost",
  "saving",
  "skill",
] as const;

export type UsageFilterDimension = (typeof USAGE_FILTER_DIMENSIONS)[number];
export type UsageFilterMode = "include" | "exclude";
export type UsageFilterValues = Partial<Record<UsageFilterDimension, string[]>>;

export interface UsageFilter {
  days: number;
  group: "daily" | "weekly";
  include: UsageFilterValues;
  exclude: UsageFilterValues;
}

export const DEFAULT_USAGE_FILTER: UsageFilter = {
  days: 30,
  group: "daily",
  include: {},
  exclude: {},
};

export function parseUsageFilter(params: URLSearchParams): UsageFilter {
  const rawDays = Number.parseInt(params.get("days") ?? "", 10);
  const days = Number.isFinite(rawDays) && rawDays > 0 && rawDays <= 365 ? rawDays : 30;
  const group = params.get("grain") === "weekly" ? "weekly" : "daily";
  const include: UsageFilterValues = {};
  const exclude: UsageFilterValues = {};

  for (const dimension of USAGE_FILTER_DIMENSIONS) {
    setValues(include, dimension, params.getAll(dimension));
    setValues(exclude, dimension, params.getAll(`exclude.${dimension}`));
  }

  return { days, group, include, exclude };
}

export function serializeUsageFilter(filter: UsageFilter): string {
  const params = new URLSearchParams();
  params.set("days", String(filter.days));
  params.set("grain", filter.group);
  for (const dimension of USAGE_FILTER_DIMENSIONS) {
    for (const value of normalize(filter.include[dimension] ?? [])) params.append(dimension, value);
  }
  for (const dimension of USAGE_FILTER_DIMENSIONS) {
    for (const value of normalize(filter.exclude[dimension] ?? [])) {
      params.append(`exclude.${dimension}`, value);
    }
  }
  return params.toString();
}

export function toggleUsageFilterValue(
  filter: UsageFilter,
  dimension: UsageFilterDimension,
  value: string,
  mode: UsageFilterMode,
): UsageFilter {
  const target = mode === "include" ? filter.include : filter.exclude;
  const other = mode === "include" ? filter.exclude : filter.include;
  const values = new Set(target[dimension] ?? []);
  if (values.has(value)) values.delete(value);
  else values.add(value);

  return {
    ...filter,
    include: updateValues(
      mode === "include" ? target : removeValue(other, dimension, value),
      dimension,
      mode === "include" ? values : new Set(removeValue(other, dimension, value)[dimension] ?? []),
    ),
    exclude: updateValues(
      mode === "exclude" ? target : removeValue(other, dimension, value),
      dimension,
      mode === "exclude" ? values : new Set(removeValue(other, dimension, value)[dimension] ?? []),
    ),
  };
}

function setValues(target: UsageFilterValues, dimension: UsageFilterDimension, values: string[]) {
  const normalized = normalize(values);
  if (normalized.length > 0) target[dimension] = normalized;
}

function normalize(values: string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].sort();
}

function removeValue(
  source: UsageFilterValues,
  dimension: UsageFilterDimension,
  value: string,
): UsageFilterValues {
  const next = { ...source };
  const values = (source[dimension] ?? []).filter((item) => item !== value);
  if (values.length > 0) next[dimension] = values;
  else delete next[dimension];
  return next;
}

function updateValues(
  source: UsageFilterValues,
  dimension: UsageFilterDimension,
  values: Set<string>,
): UsageFilterValues {
  const next = { ...source };
  const normalized = normalize([...values]);
  if (normalized.length > 0) next[dimension] = normalized;
  else delete next[dimension];
  return next;
}
