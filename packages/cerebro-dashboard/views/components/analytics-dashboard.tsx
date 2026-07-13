"use client";

import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { useMutation, useQueries, useQuery } from "@tanstack/react-query";
import {
  analyticsCatalogOptions,
  analyticsQueryOptions,
  analyticsVisualsOptions,
  createAnalyticsVisual,
  updateAnalyticsVisual,
  type AnalyticsDimension,
} from "@multica/cerebro-usage";
import {
  buildVisualQuery,
  DEFAULT_ANALYTICS_VISUALS,
  filtersFromSearchParams,
  filtersToSearchParams,
  removeAnalyticsFilterValue,
  toggleAnalyticsFilter,
  type AnalyticsFilter,
  type AnalyticsVisual,
  visualPresentationFromDisplay,
} from "../../core/analytics";
import { AnalyticsWorkbench } from "./analytics-workbench";

export function AnalyticsDashboard({ workspaceId, initialVisuals = DEFAULT_ANALYTICS_VISUALS, showToolbar = true, builderOpen, onBuilderOpenChange, filters: controlledFilters, onFiltersChange }: { workspaceId: string; initialVisuals?: AnalyticsVisual[]; showToolbar?: boolean; builderOpen?: boolean; onBuilderOpenChange?: (open: boolean) => void; filters?: AnalyticsFilter[]; onFiltersChange?: Dispatch<SetStateAction<AnalyticsFilter[]>> }) {
  const [internalFilters, setInternalFilters] = useState<AnalyticsFilter[]>(() => typeof window === "undefined" ? [] : filtersFromSearchParams(new URLSearchParams(window.location.search)));
  const filters = controlledFilters ?? internalFilters;
  const setFilters = onFiltersChange ?? setInternalFilters;
  const [visuals, setVisuals] = useState<AnalyticsVisual[]>(initialVisuals);
  const [cursorHistory, setCursorHistory] = useState<Record<string, string[]>>({});
  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);
  useEffect(() => {
    const params = filtersToSearchParams(filters, new URLSearchParams(window.location.search));
    const search = params.toString();
    window.history.replaceState(null, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [filters]);
  const catalog = useQuery(analyticsCatalogOptions(workspaceId));
  const savedVisuals = useQuery(analyticsVisualsOptions(workspaceId));
  useEffect(() => {
    if (!savedVisuals.data?.length) return;
    setVisuals((current) => {
      const defaults = current.filter((visual) => !savedVisuals.data.some((saved) => saved.id === visual.id));
      return [...defaults, ...savedVisuals.data.map((saved) => ({
        id: saved.id,
        title: saved.name,
        kind: saved.visual_type,
        presentation: visualPresentationFromDisplay(saved.display, saved.visual_type),
        metrics: saved.query.metrics ?? ["runs"],
        dimensions: saved.query.dimensions ?? ["time"],
        grain: saved.query.grain ?? "none",
        limit: saved.query.page?.limit ?? 12,
      }))];
    });
  }, [savedVisuals.data]);
  const createVisual = useMutation({
    mutationFn: createAnalyticsVisual,
    onSuccess: (saved, input) => setVisuals((current) => current.map((visual) => visual.title === input.name && visual.id.startsWith("custom-") ? { ...visual, id: saved.id } : visual)),
    onError: (_error, input) => setVisuals((current) => current.filter((visual) => !(visual.title === input.name && visual.id.startsWith("custom-")))),
  });
  const updateVisual = useMutation({ mutationFn: ({ id, input }: { id: string; input: Parameters<typeof updateAnalyticsVisual>[1] }) => updateAnalyticsVisual(id, input) });
  const queries = useQueries({
    queries: visuals.map((visual) => {
      const cursors = cursorHistory[visual.id] ?? [];
      return analyticsQueryOptions(
        workspaceId,
        buildVisualQuery(visual, filters, timezone, cursors.at(-1)),
      );
    }),
  });
  const results = Object.fromEntries(
    visuals.map((visual, index) => [visual.id, queries[index]?.data]),
  );
  const canPrevious = Object.fromEntries(
    visuals.map((visual) => [visual.id, (cursorHistory[visual.id]?.length ?? 0) > 0]),
  );
  const toggleFilter = (
    dimension: AnalyticsDimension,
    value: string,
    operator: "in" | "not_in",
  ) => {
    setFilters((current) => toggleAnalyticsFilter(current, dimension, value, operator));
    setCursorHistory({});
  };
  const removeFilter = (
    dimension: AnalyticsDimension,
    value: string,
    operator: AnalyticsFilter["operator"],
  ) => {
    setFilters((current) => removeAnalyticsFilterValue(current, dimension, value, operator));
    setCursorHistory({});
  };
  return (
    <AnalyticsWorkbench
      visuals={visuals}
      results={results}
      filters={filters}
      onFilter={toggleFilter}
      onRemoveFilter={removeFilter}
      onNext={(visualId, cursor) =>
        setCursorHistory((current) => ({
          ...current,
          [visualId]: [...(current[visualId] ?? []), cursor],
        }))
      }
      onPrevious={(visualId) =>
        setCursorHistory((current) => ({
          ...current,
          [visualId]: (current[visualId] ?? []).slice(0, -1),
        }))
      }
      canPrevious={canPrevious}
      onAddVisual={(visual) => {
        setVisuals((current) => [...current, visual]);
        createVisual.mutate({ name: visual.title, visual_type: visual.kind, query: buildVisualQuery(visual, filters, timezone), display: { presentation: visual.presentation }, position: visuals.length });
      }}
      onConfigure={(visual) => {
        setVisuals((current) => current.map((item) => item.id === visual.id ? visual : item));
        if (!DEFAULT_ANALYTICS_VISUALS.some((item) => item.id === visual.id) && !visual.id.startsWith("custom-")) {
          updateVisual.mutate({ id: visual.id, input: { name: visual.title, visual_type: visual.kind, query: buildVisualQuery(visual, filters, timezone), display: { presentation: visual.presentation }, position: visuals.findIndex((item) => item.id === visual.id) } });
        }
      }}
      catalog={catalog.data}
      showToolbar={showToolbar}
      builderOpen={builderOpen}
      onBuilderOpenChange={onBuilderOpenChange}
    />
  );
}
