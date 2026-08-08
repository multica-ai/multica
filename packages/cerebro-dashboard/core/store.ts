"use client";

import { create } from "zustand";
import type { AnalyticsFilter } from "./analytics";
import {
  clearDashboardFilters,
  DEFAULT_DASHBOARD_FILTER_STATE,
  parseDashboardFilterState,
  withAnalyticsFilters,
  type DashboardAIImpactSelections,
  type DashboardFilterState,
} from "./filter-state";
import type { ActorScope, TimeRange } from "./types";

// Client-state for the dashboard. Zustand is the in-memory source of truth;
// DashboardPage mirrors the filter subset to the URL for reloads and sharing.
// Server data remains in TanStack Query, keyed on the relevant filter values.
export type DashboardTab = "overview" | "runs" | "messages" | "ai-impact" | "people";

interface DashboardState extends DashboardFilterState {
  tab: DashboardTab;
  // Message panel: which member's messages are shown in the detail panel
  messagePanelActorId: string | null;
  messagePanelActorName: string | null;
  setScope: (scope: ActorScope) => void;
  setActor: (id: string | null, name?: string | null, type?: "member" | "agent") => void;
  setRange: (range: TimeRange) => void;
  setAnalyticsFilters: (
    filters: AnalyticsFilter[] | ((current: AnalyticsFilter[]) => AnalyticsFilter[]),
  ) => void;
  hydrateFilters: (params: URLSearchParams) => void;
  setAIImpactSelection: (selection: Partial<DashboardAIImpactSelections>) => void;
  clearFilters: () => void;
  setTab: (tab: DashboardTab) => void;
  openMessagePanel: (actorId: string, actorName: string) => void;
  closeMessagePanel: () => void;
  reset: () => void;
}

export const useDashboardStore = create<DashboardState>((set) => ({
  ...freshDefaultFilterState(),
  tab: "overview",
  messagePanelActorId: null,
  messagePanelActorName: null,
  setScope: (scope) =>
    set((state) => {
      if (state.scope === scope) return state;
      return {
        ...withAnalyticsFilters(
          state,
          state.analyticsFilters.filter(
            (filter) => filter.dimension !== "person" && filter.dimension !== "agent",
          ),
        ),
        scope,
        actorId: null,
        actorName: null,
      };
    }),
  setActor: (actorId, actorName = null, type) =>
    set((state) => {
      const scope = type === "agent" ? "agents" : type === "member" ? "members" : state.scope;
      const withoutActor = state.analyticsFilters.filter(
        (filter) => filter.dimension !== "person" && filter.dimension !== "agent",
      );
      const actorFilter: AnalyticsFilter | null =
        actorId && actorName
          ? {
              dimension: type === "agent" || scope === "agents" ? "agent" : "person",
              operator: "in",
              values: [actorName],
            }
          : null;
      const analyticsFilters =
        actorFilter
          ? [...withoutActor, actorFilter]
          : withoutActor;
      return {
        ...withAnalyticsFilters(state, analyticsFilters),
        scope,
        actorId,
        actorName,
      };
    }),
  setRange: (range) =>
    set((state) => ({
      ...withAnalyticsFilters(
        state,
        state.analyticsFilters.filter(
          (filter) =>
            !(
              filter.dimension === "time" &&
              (filter.operator === "gte" || filter.operator === "lte")
            ),
        ),
      ),
      range,
    })),
  setAnalyticsFilters: (filters) =>
    set((state) => {
      const next = withAnalyticsFilters(
        state,
        typeof filters === "function" ? filters(state.analyticsFilters) : filters,
      );
      const hasActorFilter = next.analyticsFilters.some(
        (filter) =>
          filter.dimension === (state.scope === "agents" ? "agent" : "person") &&
          filter.operator === "in" &&
          state.actorName !== null &&
          filter.values.includes(state.actorName),
      );
      return state.actorId && !hasActorFilter
        ? { ...next, actorId: null, actorName: null }
        : next;
    }),
  hydrateFilters: (params) => set(parseDashboardFilterState(params)),
  setAIImpactSelection: (selection) =>
    set((state) => ({
      aiImpactSelections: { ...state.aiImpactSelections, ...selection },
    })),
  clearFilters: () => set((state) => clearDashboardFilters(state)),
  setTab: (tab) => set({ tab }),
  openMessagePanel: (messagePanelActorId, messagePanelActorName) =>
    set({ messagePanelActorId, messagePanelActorName }),
  closeMessagePanel: () => set({ messagePanelActorId: null, messagePanelActorName: null }),
  reset: () =>
    set({
      ...freshDefaultFilterState(),
      tab: "overview",
      messagePanelActorId: null,
      messagePanelActorName: null,
    }),
}));

function freshDefaultFilterState(): DashboardFilterState {
  return {
    ...DEFAULT_DASHBOARD_FILTER_STATE,
    analyticsFilters: [],
    aiImpactSelections: { ...DEFAULT_DASHBOARD_FILTER_STATE.aiImpactSelections },
  };
}
