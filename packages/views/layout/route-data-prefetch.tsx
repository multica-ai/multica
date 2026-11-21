"use client";

import { useCallback, useEffect, useMemo, useRef, type ReactNode } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  agentActivity30dOptions,
  agentRunCounts30dOptions,
  agentTaskSnapshotOptions,
} from "@multica/core/agents/queries";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { issueDetailOptions, issueKeys, prefetchIssuePage } from "@multica/core/issues/queries";
import type { Issue } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useIsGuest } from "@multica/core/permissions";
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

/** A pointer passing over a list of issues is not heading for every row in it. */
const ISSUE_HOVER_INTENT_MS = 80;

/**
 * The issue a link names by UUID or identifier, also cached under its UUID:
 * the page keys everything else by UUID, and resolves an identifier through
 * the identifier-keyed entry this fills.
 */
async function fetchIssue(qc: QueryClient, wsId: string, ref: string): Promise<Issue> {
  const issue = await qc.fetchQuery(issueDetailOptions(wsId, ref));
  if (issue.id !== ref) {
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issue.id), (old) => old ?? issue);
  }
  return issue;
}

// A guest (MUL-9420) only reaches these pages.
const GUEST_ROUTES: RouteKey[] = ["projects", "inbox"];

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
 * Feeds `AppLink`'s hover/focus prefetch with the destination page's data, an
 * issue page's included, and warms the common list pages once the workspace
 * is idle. Mount inside the workspace shell, around the navigation that
 * should benefit.
 */
export function RouteDataPrefetch({
  wsId,
  userId,
  guest = false,
  children,
}: {
  wsId: string | undefined;
  userId: string | null;
  guest?: boolean;
  children: ReactNode;
}) {
  const qc = useQueryClient();
  // Null-safe: the desktop shell mounts before a workspace is open.
  const slug = useWorkspaceSlug();

  const routeByHref = useMemo(() => {
    if (!slug) return new Map<string, WarmRoute>();
    const p = paths.workspace(slug);
    const keys = guest ? GUEST_ROUTES : (Object.keys(ROUTES) as RouteKey[]);
    return new Map<string, WarmRoute>(keys.map((key) => [p[key](), ROUTES[key]]));
  }, [slug, guest]);

  const issuePrefix = slug ? `${paths.workspace(slug).issues()}/` : null;
  const issueTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (issueTimer.current) clearTimeout(issueTimer.current);
  }, []);

  const prefetch = useCallback(
    (href: string) => {
      if (!wsId) return;
      const path = href.split(/[?#]/)[0] ?? href;
      const segment = issuePrefix && path.startsWith(issuePrefix) ? path.slice(issuePrefix.length) : "";
      if (segment && !segment.includes("/")) {
        // The issue alone holds the page's first paint, so it is fetched at
        // once; what the page fills in after it waits for the pointer to
        // settle. A failed warm-up is the page's own first request again.
        if (issueTimer.current) clearTimeout(issueTimer.current);
        let ref: string;
        try {
          ref = decodeURIComponent(segment);
        } catch {
          return;
        }
        const issue = fetchIssue(qc, wsId, ref);
        issue.catch(() => undefined);
        // A guest's issue page reads the issue alone (MUL-9420).
        if (guest) return;
        issueTimer.current = setTimeout(() => {
          issueTimer.current = null;
          issue.then((loaded) => prefetchIssuePage(qc, wsId, loaded.id)).catch(() => undefined);
        }, ISSUE_HOVER_INTENT_MS);
        return;
      }
      routeByHref.get(path)?.(qc, wsId, userId);
    },
    [qc, routeByHref, issuePrefix, wsId, userId, guest],
  );

  useEffect(() => {
    if (!wsId) return;
    const warm = () => {
      for (const key of guest ? GUEST_ROUTES : WARM_ON_IDLE) ROUTES[key](qc, wsId, userId);
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(warm, { timeout: 3000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = window.setTimeout(warm, 1500);
    return () => window.clearTimeout(timer);
  }, [qc, wsId, userId, guest]);

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
  const guest = useIsGuest(wsId);
  return (
    <RouteDataPrefetch wsId={wsId} userId={userId} guest={guest}>
      {children}
    </RouteDataPrefetch>
  );
}
