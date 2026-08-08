import type { AnalyticsDimension } from "@multica/cerebro-usage";
import {
  ANALYTICS_FILTER_DIMENSIONS,
  type AnalyticsFilter,
} from "./analytics";
import type {
  DashboardAIImpactSelections,
  DashboardExactTimeRange,
  DashboardFilterState,
} from "./filter-state";
import type { DashboardTab } from "./store";
import type { ActorScope, TimeRange } from "./types";

export type DashboardFilterAdapter =
  | "analytics"
  | "dashboardOverview"
  | "dashboardMessages"
  | "aiImpact"
  | "people";

export type DashboardFilterTimeCapability = "preset" | "exact";
export type DashboardFilterActorCapability = "scope" | "id";
export type DashboardAIImpactCapability = keyof DashboardAIImpactSelections;

export interface DashboardFilterCapabilities {
  time: readonly DashboardFilterTimeCapability[];
  actor: readonly DashboardFilterActorCapability[];
  analytics: readonly AnalyticsDimension[];
  aiImpact: readonly DashboardAIImpactCapability[];
}

export const DASHBOARD_FILTER_CAPABILITIES = {
  analytics: {
    time: ["exact"],
    actor: [],
    analytics: ANALYTICS_FILTER_DIMENSIONS,
    aiImpact: [],
  },
  dashboardOverview: {
    time: ["preset", "exact"],
    actor: ["scope", "id"],
    analytics: [],
    aiImpact: [],
  },
  dashboardMessages: {
    time: ["preset", "exact"],
    actor: ["scope", "id"],
    analytics: [],
    aiImpact: [],
  },
  aiImpact: {
    time: [],
    actor: [],
    analytics: [],
    aiImpact: [],
  },
  people: {
    time: [],
    actor: [],
    analytics: [],
    aiImpact: [],
  },
} as const satisfies Record<DashboardFilterAdapter, DashboardFilterCapabilities>;

export const DASHBOARD_PANEL_FILTER_ADAPTERS = {
  overview: ["dashboardOverview", "analytics"],
  runs: ["analytics"],
  messages: ["dashboardOverview", "dashboardMessages", "analytics"],
  "ai-impact": ["aiImpact"],
  people: ["people"],
} as const satisfies Record<DashboardTab, readonly DashboardFilterAdapter[]>;

export interface DashboardRangeActorFilters {
  range: TimeRange;
  scope: ActorScope;
  actorId: string | null;
  exactTimeRange: DashboardExactTimeRange | null;
}

export type SupportedDashboardAdapterFilters<T> = {
  supported: true;
  filters: T;
};

export type UnsupportedDashboardAdapterFilters = {
  supported: false;
  filters: null;
  reason: string;
};

const ANALYTICS_FILTER_DIMENSION_SET = new Set<AnalyticsDimension>(
  DASHBOARD_FILTER_CAPABILITIES.analytics.analytics,
);

export function selectAnalyticsAdapterFilters(
  state: DashboardFilterState,
): SupportedDashboardAdapterFilters<AnalyticsFilter[]> {
  return {
    supported: true,
    filters: state.analyticsFilters.filter((filter) =>
      ANALYTICS_FILTER_DIMENSION_SET.has(filter.dimension),
    ),
  };
}

export function selectDashboardOverviewAdapterFilters(
  state: DashboardFilterState,
): SupportedDashboardAdapterFilters<DashboardRangeActorFilters> {
  return selectRangeActorFilters(state);
}

export function selectDashboardMessagesAdapterFilters(
  state: DashboardFilterState,
): SupportedDashboardAdapterFilters<DashboardRangeActorFilters> {
  return selectRangeActorFilters(state);
}

export function selectAIImpactAdapterFilters(
  _state: DashboardFilterState,
): UnsupportedDashboardAdapterFilters {
  return {
    supported: false,
    filters: null,
    reason: "Shared Dashboard filters are not supported by the current AI Impact API.",
  };
}

export function selectPeopleAdapterFilters(
  _state: DashboardFilterState,
): UnsupportedDashboardAdapterFilters {
  return {
    supported: false,
    filters: null,
    reason: "Shared Dashboard filters are not supported by the current People API.",
  };
}

function selectRangeActorFilters(
  state: DashboardFilterState,
): SupportedDashboardAdapterFilters<DashboardRangeActorFilters> {
  return {
    supported: true,
    filters: {
      range: state.range,
      scope: state.scope,
      actorId: state.actorId,
      exactTimeRange: state.exactTimeRange,
    },
  };
}
