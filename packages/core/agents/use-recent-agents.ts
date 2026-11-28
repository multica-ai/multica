"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { api } from "../api";
import type { RecentAgentSelectionResponse } from "../types";
import {
  recentAgentSelectionKeys,
  recentAgentSelectionOptions,
} from "./queries";
import { MAX_RECENT_AGENTS } from "./recent-agent-selection";

const EMPTY_AGENT_IDS: string[] = [];

export type RecentAgentRecordErrorHandler = (
  error: Error,
  retry: () => void,
) => void;

interface UseRecentAgentsOptions<T extends { id: string }> {
  agents: readonly T[];
  userId: string | null | undefined;
  wsId: string | null | undefined;
  enabled: boolean;
  onRecordError?: RecentAgentRecordErrorHandler;
}

export function useRecentAgents<T extends { id: string }>({
  agents,
  userId,
  wsId,
  enabled,
  onRecordError,
}: UseRecentAgentsOptions<T>) {
  const queryClient = useQueryClient();
  const scopedUserId = userId ?? "";
  const scopedWsId = wsId ?? "";
  const queryKey = recentAgentSelectionKeys.current(scopedUserId, scopedWsId);
  const query = useQuery({
    ...recentAgentSelectionOptions(scopedUserId, scopedWsId),
    enabled: enabled && scopedUserId.length > 0 && scopedWsId.length > 0,
  });
  const { mutate } = useMutation({
    mutationKey: [...queryKey, "record"],
    scope: { id: `recent-agents:${scopedUserId}:${scopedWsId}` },
    mutationFn: (agentId: string) =>
      api.recordRecentAgentSelection(agentId, scopedWsId),
    onSuccess: (response) => {
      queryClient.setQueryData<RecentAgentSelectionResponse>(
        queryKey,
        response,
      );
    },
  });
  const recentAgentIds =
    enabled && scopedUserId && scopedWsId
      ? (query.data?.agentIds ?? EMPTY_AGENT_IDS)
      : EMPTY_AGENT_IDS;
  const recentAgents = useMemo(
    () => resolveRecentAgents(recentAgentIds, agents),
    [agents, recentAgentIds],
  );
  const recentIds = useMemo(
    () => new Set(recentAgents.map((agent) => agent.id)),
    [recentAgents],
  );
  const recordRecentAgent = useCallback(
    (agentId: string) => {
      if (!enabled || !userId || !wsId) return;
      const submit = () => {
        mutate(agentId, {
          onError: (error) => onRecordError?.(error, submit),
        });
      };
      submit();
    },
    [enabled, mutate, onRecordError, userId, wsId],
  );

  return {
    recentAgents,
    recentIds,
    recordRecentAgent,
    isRecentAgentsError: query.isError,
    refetchRecentAgents: query.refetch,
  };
}

export function resolveRecentAgents<T extends { id: string }>(
  recentAgentIds: readonly string[],
  availableAgents: readonly T[],
): T[] {
  const availableById = new Map(
    availableAgents.map((agent) => [agent.id, agent]),
  );
  const seen = new Set<string>();
  const resolved: T[] = [];
  for (const id of recentAgentIds) {
    if (seen.has(id)) continue;
    seen.add(id);
    const agent = availableById.get(id);
    if (agent) resolved.push(agent);
    if (resolved.length === MAX_RECENT_AGENTS) break;
  }
  return resolved;
}
