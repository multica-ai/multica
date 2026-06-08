"use client";

import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { WorkspaceIssueStatusResponse } from "../api/client";
import { useWorkspaceId } from "../hooks";
import {
  DEFAULT_ALL_STATUSES,
  DEFAULT_STATUS_CONFIG,
  type StatusDefinition,
  type StatusConfigEntry,
} from "./config";

// ---------------------------------------------------------------------------
// Query keys & options
// ---------------------------------------------------------------------------

export const workspaceStatusKeys = {
  all: (wsId: string) => ["workspaces", wsId, "issue-statuses"] as const,
};

/** Convert API response to the internal StatusDefinition shape. */
function toStatusDefinition(r: WorkspaceIssueStatusResponse): StatusDefinition {
  return {
    name: r.name,
    label: r.label,
    color: r.color,
    category: r.category,
    position: r.position,
    isDefault: r.is_default,
  };
}

/** Default StatusDefinition list derived from the built-in config.
 *  Used as fallback when the API call fails or returns empty. */
const DEFAULT_STATUS_DEFINITIONS: StatusDefinition[] = DEFAULT_ALL_STATUSES.map(
  (name, i) => {
    const cfg = DEFAULT_STATUS_CONFIG[name];
    return {
      name,
      label: cfg.label,
      color: categoryToDefaultColor(categoryForDefault(name)),
      category: categoryForDefault(name),
      position: i,
      isDefault: true,
    };
  },
);

function categoryForDefault(
  name: string,
): StatusDefinition["category"] {
  switch (name) {
    case "backlog":
    case "todo":
      return "not_started";
    case "in_progress":
    case "in_review":
    case "blocked":
      return "started";
    case "done":
      return "completed";
    case "cancelled":
      return "cancelled";
    default:
      return "not_started";
  }
}

function categoryToDefaultColor(category: StatusDefinition["category"]): string {
  switch (category) {
    case "not_started":
      return "#6b7280";
    case "started":
      return "#f59e0b";
    case "completed":
      return "#3b82f6";
    case "cancelled":
      return "#6b7280";
  }
}

export function workspaceIssueStatusesOptions(wsId: string) {
  return queryOptions<StatusDefinition[]>({
    queryKey: workspaceStatusKeys.all(wsId),
    queryFn: async () => {
      const raw = await api.listWorkspaceIssueStatuses(wsId);
      if (!raw || raw.length === 0) return DEFAULT_STATUS_DEFINITIONS;
      return raw.map(toStatusDefinition).sort((a, b) => a.position - b.position);
    },
    // Status config rarely changes — keep it cached for 5 minutes.
    staleTime: 5 * 60 * 1000,
    // Gracefully degrade: on network error the UI falls back to defaults.
    placeholderData: DEFAULT_STATUS_DEFINITIONS,
  });
}

// ---------------------------------------------------------------------------
// React hook
// ---------------------------------------------------------------------------

export interface UseWorkspaceStatusesResult {
  /** All statuses for the current workspace, sorted by position. */
  statuses: StatusDefinition[];
  /** Map from status name → StatusConfigEntry for UI theming. */
  configMap: Record<string, StatusConfigEntry>;
  /** Whether the query is still loading for the first time. */
  isLoading: boolean;
}

/**
 * Fetches the workspace's custom issue statuses via React Query.
 * Falls back to the built-in DEFAULT statuses if the API call fails.
 */
export function useWorkspaceStatuses(): UseWorkspaceStatusesResult {
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(workspaceIssueStatusesOptions(wsId));

  const statuses = data ?? DEFAULT_STATUS_DEFINITIONS;

  // Build a merged config map: built-in defaults + custom statuses get
  // a synthesised StatusConfigEntry based on their color.
  const configMap: Record<string, StatusConfigEntry> = { ...DEFAULT_STATUS_CONFIG };
  for (const s of statuses) {
    if (!(s.name in configMap)) {
      configMap[s.name] = {
        label: s.label,
        iconColor: "text-muted-foreground",
        hoverBg: "hover:bg-accent",
        dividerColor: "bg-muted-foreground/40",
        columnBg: "bg-muted/40",
      };
    }
  }

  return { statuses, configMap, isLoading };
}
