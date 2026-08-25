"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  AnalyticsParams,
  AnalyticsResult,
  AnalyticsWorkspaceBreakdownParams,
  AnalyticsWorkspaceBreakdownResult,
  ListWorkspacesParams,
  ListWorkspacesResult,
  WorkspaceDetail,
} from "./types";
import type { Invitation } from "./agentfarm-schema";
import { parseAnalyticsWorkspaceBreakdown } from "./analytics-schema";

// Client-side data hooks. These hit this app's own Route Handlers (never
// Postgres/LiteLLM directly, never the Go API) — see app/api/workspaces/*.

async function fetchJson<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
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

export function useAnalyticsWorkspaceBreakdown(params: AnalyticsWorkspaceBreakdownParams | null) {
  const qs = params
    ? new URLSearchParams({
        from: params.from,
        to: params.to,
        kind: params.kind,
        segment: params.segment,
      })
    : null;
  return useQuery<AnalyticsWorkspaceBreakdownResult>({
    queryKey: ["analytics-workspace-breakdown", params],
    queryFn: async () => parseAnalyticsWorkspaceBreakdown(await fetchJson(`/api/analytics/workspaces?${qs?.toString()}`)),
    enabled: params !== null,
  });
}

interface InviteMemberInput {
  email: string;
  role: "admin" | "member";
}

// Route handler runs the real (LBYL) pre-checks fresh against the DB right
// before calling the Go API — see app/api/workspaces/[id]/invitations/route.ts.
// This hook just posts and re-syncs the panel: on success the workspace query
// is invalidated so the new pending invitation shows up without a manual refresh.
export function useInviteMember(workspaceId: string | null) {
  const queryClient = useQueryClient();
  return useMutation<Invitation, Error, InviteMemberInput>({
    mutationFn: (input) => {
      if (workspaceId === null) {
        return Promise.reject(new Error("Cannot invite a member: no workspace is selected"));
      }
      return fetchJson(`/api/workspaces/${workspaceId}/invitations`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["workspace", workspaceId] });
    },
  });
}
