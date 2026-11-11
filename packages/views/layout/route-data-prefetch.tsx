"use client";

import { useCallback, useEffect, useMemo, type ReactNode } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  agentActivity30dOptions,
  agentRunCounts30dOptions,
  agentTaskSnapshotOptions,
} from "@multica/core/agents/queries";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useAuthStore } from "@multica/core/auth";
import { paths, useCurrentWorkspace, useWorkspaceSlug } from "@multica/core/paths";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { RouteDataPrefetchProvider } from "../navigation/route-data-prefetch";

type WarmRoute = (qc: QueryClient, wsId: string, userId: string | null) => void;

// Each entry warms what its page holds its first paint on — the list behind
// the skeleton plus the queries its default sort or columns gate on (Agents'
// lastActive sort waits on activity + run counts). An entry that drifts from
// its page just prefetches the wrong keys, so keep them in step.
// prefetchQuery honours each query's staleTime, so a warm cache costs nothing.
const ROUTES = {
  agents: (qc, wsId) => {
    void qc.prefetchQuery(agentListOptions(wsId));
    void qc.prefetchQuery(runtimeListOptions(wsId));
    void qc.prefetchQuery(memberListOptions(wsId));
    void qc.prefetchQuery(agentRunCounts30dOptions(wsId));
    void qc.prefetchQuery(agentActivity30dOptions(wsId));
    void qc.prefetchQuery(agentTaskSnapshotOptions(wsId));
  },
  skills: (qc, wsId) => {
    void qc.prefetchQuery(skillListOptions(wsId));
    void qc.prefetchQuery(agentListOptions(wsId));
    void qc.prefetchQuery(memberListOptions(wsId));
    void qc.prefetchQuery(runtimeListOptions(wsId));
  },
  squads: (qc, wsId) => {
    void qc.prefetchQuery(squadListOptions(wsId));
    void qc.prefetchQuery(agentListOptions(wsId));
    void qc.prefetchQuery(memberListOptions(wsId));
  },
  autopilots: (qc, wsId) => {
    void qc.prefetchQuery(autopilotListOptions(wsId));
  },
  projects: (qc, wsId, userId) => {
    void qc.prefetchQuery(projectListOptions(wsId));
    void qc.prefetchQuery(memberListOptions(wsId));
    if (userId) void qc.prefetchQuery(pinListOptions(wsId, userId));
  },
  runtimes: (qc, wsId) => {
    void qc.prefetchQuery(runtimeListOptions(wsId));
    void qc.prefetchQuery(runtimeProfileListOptions(wsId));
    void qc.prefetchQuery(agentListOptions(wsId));
    void qc.prefetchQuery(chatSessionsOptions(wsId));
  },
  inbox: (qc, wsId) => {
    void qc.prefetchQuery(inboxListOptions(wsId));
  },
} satisfies Record<string, WarmRoute>;

type RouteKey = keyof typeof ROUTES;

// Small, workspace-scoped lists worth fetching before anyone asks, so the
// first visit to each is already warm. Unobserved queries are collected after
// their gcTime; hover/focus prefetch covers visits after that.
const WARM_ON_IDLE: RouteKey[] = [
  "agents",
  "skills",
  "squads",
  "autopilots",
  "projects",
];

/**
 * Feeds `AppLink`'s hover/focus prefetch with the destination page's data and
 * warms the common list pages once the workspace is idle. Mount inside the
 * workspace shell, around the navigation that should benefit.
 */
export function RouteDataPrefetch({
  wsId,
  userId,
  children,
}: {
  wsId: string | undefined;
  userId: string | null;
  children: ReactNode;
}) {
  const qc = useQueryClient();
  // Null-safe: the desktop shell mounts before a workspace is open.
  const slug = useWorkspaceSlug();

  const routeByHref = useMemo(() => {
    if (!slug) return new Map<string, WarmRoute>();
    const p = paths.workspace(slug);
    return new Map<string, WarmRoute>(
      (Object.keys(ROUTES) as RouteKey[]).map((key) => [p[key](), ROUTES[key]]),
    );
  }, [slug]);

  const prefetch = useCallback(
    (href: string) => {
      if (!wsId) return;
      const path = href.split(/[?#]/)[0] ?? href;
      routeByHref.get(path)?.(qc, wsId, userId);
    },
    [qc, routeByHref, wsId, userId],
  );

  useEffect(() => {
    if (!wsId) return;
    const warm = () => {
      for (const key of WARM_ON_IDLE) ROUTES[key](qc, wsId, userId);
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(warm, { timeout: 3000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = window.setTimeout(warm, 1500);
    return () => window.clearTimeout(timer);
  }, [qc, wsId, userId]);

  return (
    <RouteDataPrefetchProvider value={prefetch}>
      {children}
    </RouteDataPrefetchProvider>
  );
}

/** `RouteDataPrefetch` for the app shell, scoped to the open workspace and user. */
export function ShellRouteDataPrefetch({ children }: { children: ReactNode }) {
  const wsId = useCurrentWorkspace()?.id;
  const userId = useAuthStore((s) => s.user?.id ?? null);
  return (
    <RouteDataPrefetch wsId={wsId} userId={userId}>
      {children}
    </RouteDataPrefetch>
  );
}
