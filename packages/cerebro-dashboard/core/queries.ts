import { queryOptions } from "@tanstack/react-query";
import {
  fetchDashboardOverview,
  fetchActorMessages,
  fetchAllMessages,
  fetchSessionMessages,
  type TimeRange,
} from "./api";
import type { ActorScope } from "./types";

export const dashboardKeys = {
  all: (wsId: string) => ["cerebro", "dashboard", wsId] as const,
  overview: (wsId: string, range: TimeRange, scope: ActorScope, actorId: string | null) =>
    [...dashboardKeys.all(wsId), "overview", range, scope, actorId] as const,
  actorMessages: (wsId: string, actorId: string, range: TimeRange) =>
    [...dashboardKeys.all(wsId), "actor-messages", actorId, range] as const,
  allMessages: (wsId: string, range: TimeRange, scope: ActorScope, actorId: string | null) =>
    [...dashboardKeys.all(wsId), "all-messages", range, scope, actorId] as const,
  sessionMessages: (wsId: string, sessionId: string) =>
    [...dashboardKeys.all(wsId), "session-messages", sessionId] as const,
};

export function dashboardOverviewOptions(
  wsId: string,
  range: TimeRange,
  scope: ActorScope,
  actorId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.overview(wsId, range, scope, actorId),
    queryFn: () => fetchDashboardOverview(range, scope, actorId),
    enabled: !!wsId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function actorMessagesOptions(wsId: string, actorId: string | null, range: TimeRange) {
  return queryOptions({
    queryKey: dashboardKeys.actorMessages(wsId, actorId ?? "", range),
    queryFn: () => fetchActorMessages(actorId!, range),
    enabled: !!wsId && !!actorId,
    staleTime: 30 * 1000,
  });
}

export function allMessagesOptions(
  wsId: string,
  range: TimeRange,
  scope: ActorScope,
  actorId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.allMessages(wsId, range, scope, actorId),
    queryFn: () => fetchAllMessages(range, scope, actorId),
    enabled: !!wsId,
    staleTime: 30 * 1000,
  });
}

export function sessionMessagesOptions(wsId: string, sessionId: string | null) {
  return queryOptions({
    queryKey: dashboardKeys.sessionMessages(wsId, sessionId ?? ""),
    queryFn: () => fetchSessionMessages(sessionId!),
    enabled: !!wsId && !!sessionId,
    staleTime: 30 * 1000,
  });
}
