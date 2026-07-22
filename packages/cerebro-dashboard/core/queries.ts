import { queryOptions } from "@tanstack/react-query";
import {
  fetchDashboardOverview,
  fetchActorMessages,
  fetchAllMessages,
  fetchSessionMessages,
  type TimeRange,
} from "./api";
import {
  fetchAIImpactFunctions,
  fetchAIImpactOverview,
  fetchAIImpactQualityRisk,
} from "./ai-impact-api";
import type { ActorScope } from "./types";

export const dashboardKeys = {
  all: (wsId: string) => ["cerebro", "dashboard", wsId] as const,
  aiImpact: (wsId: string) => [...dashboardKeys.all(wsId), "ai-impact"] as const,
  aiImpactOverview: (wsId: string) => [...dashboardKeys.aiImpact(wsId), "overview"] as const,
  aiImpactFunctions: (wsId: string) => [...dashboardKeys.aiImpact(wsId), "functions"] as const,
  aiImpactQualityRisk: (wsId: string) =>
    [...dashboardKeys.aiImpact(wsId), "quality-risk"] as const,
  overview: (wsId: string, range: TimeRange, scope: ActorScope, actorId: string | null) =>
    [...dashboardKeys.all(wsId), "overview", range, scope, actorId] as const,
  actorMessages: (wsId: string, actorId: string, range: TimeRange) =>
    [...dashboardKeys.all(wsId), "actor-messages", actorId, range] as const,
  allMessages: (wsId: string, range: TimeRange, scope: ActorScope, actorId: string | null) =>
    [...dashboardKeys.all(wsId), "all-messages", range, scope, actorId] as const,
  sessionMessages: (wsId: string, sessionId: string) =>
    [...dashboardKeys.all(wsId), "session-messages", sessionId] as const,
};

const AI_IMPACT_STALE_TIME = 30 * 1000;

export function aiImpactOverviewOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.aiImpactOverview(wsId),
    queryFn: fetchAIImpactOverview,
    enabled: !!wsId,
    staleTime: AI_IMPACT_STALE_TIME,
  });
}

export function aiImpactFunctionsOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.aiImpactFunctions(wsId),
    queryFn: fetchAIImpactFunctions,
    enabled: !!wsId,
    staleTime: AI_IMPACT_STALE_TIME,
  });
}

export function aiImpactQualityRiskOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.aiImpactQualityRisk(wsId),
    queryFn: fetchAIImpactQualityRisk,
    enabled: !!wsId,
    staleTime: AI_IMPACT_STALE_TIME,
  });
}

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
