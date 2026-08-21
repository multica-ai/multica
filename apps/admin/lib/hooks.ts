"use client";

import { useQuery } from "@tanstack/react-query";
import type { AnalyticsParams, AnalyticsResult, ListWorkspacesParams, ListWorkspacesResult, WorkspaceDetail } from "./types";

// Client-side data hooks. These hit this app's own Route Handlers (never
// Postgres/LiteLLM directly, never the Go API) — see app/api/workspaces/*.

async function fetchJson<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `Request failed: ${res.status}`);
  }
  return res.json();
}

export function useWorkspaceList(params: ListWorkspacesParams) {
  const qs = new URLSearchParams({
    search: params.search,
    status: params.status,
    sort: params.sort,
    direction: params.direction,
    page: String(params.page),
    pageSize: String(params.pageSize),
  });
  if (params.activityFrom) qs.set("activityFrom", params.activityFrom);
  if (params.activityTo) qs.set("activityTo", params.activityTo);
  return useQuery<ListWorkspacesResult>({
    queryKey: ["workspaces", params],
    queryFn: () => fetchJson(`/api/workspaces?${qs.toString()}`),
    placeholderData: (prev) => prev,
  });
}

export function useWorkspaceDetail(id: string | null) {
  return useQuery<WorkspaceDetail>({
    queryKey: ["workspace", id],
    queryFn: () => fetchJson(`/api/workspaces/${id}`),
    enabled: id !== null,
  });
}

export function useLiteLlmHealth() {
  return useQuery<{ configured: boolean }>({
    queryKey: ["litellm-health"],
    queryFn: () => fetchJson("/api/litellm/health"),
    staleTime: 5 * 60_000,
  });
}

export function useAnalytics(params: AnalyticsParams) {
  const qs = new URLSearchParams({
    from: params.from,
    to: params.to,
    granularityHours: String(params.granularityHours),
  });
  return useQuery<AnalyticsResult>({
    queryKey: ["analytics", params],
    queryFn: () => fetchJson(`/api/analytics?${qs.toString()}`),
    placeholderData: (prev) => prev,
  });
}
