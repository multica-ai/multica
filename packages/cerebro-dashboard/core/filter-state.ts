import type { AnalyticsDimension, AnalyticsOperator } from "@multica/cerebro-usage";
import {
  addAnalyticsFilterValue,
  filtersFromSearchParams,
  filtersToSearchParams,
  removeAnalyticsFilterValue,
  type AnalyticsFilter,
} from "./analytics";
import type { ActorScope, TimeRange } from "./types";

export interface DashboardExactTimeRange {
  start: string;
  end: string;
}

export interface DashboardAIImpactSelections {
  functionName: string | null;
  operatingLoop: string | null;
  decision: string | null;
  personType: "member" | "agent" | null;
  personId: string | null;
}

export interface DashboardFilterState {
  range: TimeRange;
  exactTimeRange: DashboardExactTimeRange | null;
  scope: ActorScope;
  actorId: string | null;
  actorName: string | null;
  analyticsFilters: AnalyticsFilter[];
  aiImpactSelections: DashboardAIImpactSelections;
}

export const DEFAULT_DASHBOARD_FILTER_STATE: DashboardFilterState = {
  range: "30d",
  exactTimeRange: null,
  scope: "all",
  actorId: null,
  actorName: null,
  analyticsFilters: [],
  aiImpactSelections: {
    functionName: null,
    operatingLoop: null,
    decision: null,
    personType: null,
    personId: null,
  },
};

const RANGE_VALUES = new Set<TimeRange>(["24h", "7d", "30d"]);
const SCOPE_VALUES = new Set<ActorScope>(["all", "members", "agents"]);
const PERSON_TYPE_VALUES = new Set<NonNullable<DashboardAIImpactSelections["personType"]>>([
  "member",
  "agent",
]);

export function parseDashboardFilterState(params: URLSearchParams): DashboardFilterState {
  let analyticsFilters = filtersFromSearchParams(params);
  const rangeValue = params.get("range");
  const scopeValue = params.get("dashboard.scope");
  const personTypeValue = params.get("ai.person_type");
  const scope = isSetValue(SCOPE_VALUES, scopeValue) ? scopeValue : "all";
  const actorId = params.get("dashboard.actor");
  const actorName = params.get("dashboard.actor_name");
  if (actorId && actorName) {
    const actorDimension = scope === "agents" ? "agent" : "person";
    const hasActorFilter = analyticsFilters.some(
      (filter) =>
        filter.dimension === actorDimension &&
        filter.operator === "in" &&
        filter.values.includes(actorName),
    );
    if (!hasActorFilter) {
      analyticsFilters = addAnalyticsFilterValue(
        analyticsFilters,
        actorDimension,
        actorName,
        "in",
      );
    }
  }

  return {
    range: isSetValue(RANGE_VALUES, rangeValue) ? rangeValue : "30d",
    exactTimeRange: exactTimeRangeFromFilters(analyticsFilters),
    scope,
    actorId,
    actorName,
    analyticsFilters,
    aiImpactSelections: {
      functionName: params.get("ai.function"),
      operatingLoop: params.get("ai.operating_loop"),
      decision: params.get("ai.decision"),
      personType: isSetValue(PERSON_TYPE_VALUES, personTypeValue) ? personTypeValue : null,
      personId: params.get("ai.person_id"),
    },
  };
}

export function serializeDashboardFilterState(
  state: DashboardFilterState,
  current = new URLSearchParams(),
): URLSearchParams {
  const params = filtersToSearchParams(state.analyticsFilters, current);
  for (const key of [
    "range",
    "dashboard.scope",
    "dashboard.actor",
    "dashboard.actor_name",
    "ai.function",
    "ai.operating_loop",
    "ai.decision",
    "ai.person_type",
    "ai.person_id",
  ]) {
    params.delete(key);
  }

  params.set("range", state.range);
  if (state.scope !== "all") params.set("dashboard.scope", state.scope);
  if (state.actorId) params.set("dashboard.actor", state.actorId);
  if (state.actorName) params.set("dashboard.actor_name", state.actorName);
  setOptionalParam(params, "ai.function", state.aiImpactSelections.functionName);
  setOptionalParam(params, "ai.operating_loop", state.aiImpactSelections.operatingLoop);
  setOptionalParam(params, "ai.decision", state.aiImpactSelections.decision);
  setOptionalParam(params, "ai.person_type", state.aiImpactSelections.personType);
  setOptionalParam(params, "ai.person_id", state.aiImpactSelections.personId);
  return params;
}

export function addDashboardFilter(
  state: DashboardFilterState,
  dimension: AnalyticsDimension,
  value: string,
  operator: Extract<AnalyticsOperator, "in" | "not_in" | "contains" | "not_contains">,
): DashboardFilterState {
  return withAnalyticsFilters(
    state,
    addAnalyticsFilterValue(state.analyticsFilters, dimension, value, operator),
  );
}

export function removeDashboardFilter(
  state: DashboardFilterState,
  dimension: AnalyticsDimension,
  value: string,
  operator: AnalyticsOperator,
): DashboardFilterState {
  return withAnalyticsFilters(
    state,
    removeAnalyticsFilterValue(state.analyticsFilters, dimension, value, operator),
  );
}

export function replaceDashboardTimeRange(
  state: DashboardFilterState,
  start: string,
  end: string,
): DashboardFilterState {
  const analyticsFilters = [
    ...state.analyticsFilters.filter(
      (filter) =>
        !(
          filter.dimension === "time" &&
          (filter.operator === "gte" || filter.operator === "lte")
        ),
    ),
    { dimension: "time" as const, operator: "gte" as const, values: [start] },
    { dimension: "time" as const, operator: "lte" as const, values: [end] },
  ];
  return { ...state, exactTimeRange: { start, end }, analyticsFilters };
}

export function clearDashboardFilters(_state: DashboardFilterState): DashboardFilterState {
  return {
    ...DEFAULT_DASHBOARD_FILTER_STATE,
    analyticsFilters: [],
    aiImpactSelections: { ...DEFAULT_DASHBOARD_FILTER_STATE.aiImpactSelections },
  };
}

export function withAnalyticsFilters(
  state: DashboardFilterState,
  analyticsFilters: AnalyticsFilter[],
): DashboardFilterState {
  return {
    ...state,
    analyticsFilters,
    exactTimeRange: exactTimeRangeFromFilters(analyticsFilters),
  };
}

function exactTimeRangeFromFilters(
  filters: AnalyticsFilter[],
): DashboardExactTimeRange | null {
  const start = filters.find(
    (filter) => filter.dimension === "time" && filter.operator === "gte",
  )?.values[0];
  const end = filters.find(
    (filter) => filter.dimension === "time" && filter.operator === "lte",
  )?.values[0];
  return start && end ? { start, end } : null;
}

function isSetValue<T extends string>(values: Set<T>, value: string | null): value is T {
  return value !== null && values.has(value as T);
}

function setOptionalParam(
  params: URLSearchParams,
  key: string,
  value: string | null,
): void {
  if (value) params.set(key, value);
}
