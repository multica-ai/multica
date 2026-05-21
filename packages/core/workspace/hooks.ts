"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "../hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "./queries";

export function useActorName() {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));

  const getMemberName = useCallback((userId: string) => {
    const m = members.find((m) => m.user_id === userId);
    return m?.name ?? "Unknown";
  }, [members]);

  const getAgentName = useCallback((agentId: string) => {
    const a = agents.find((a) => a.id === agentId);
    return a?.name ?? "Unknown Agent";
  }, [agents]);

  const getSquadName = useCallback((squadId: string) => {
    const s = squads.find((s) => s.id === squadId);
    return s?.name ?? "Unknown Squad";
  }, [squads]);

  const getActorName = useCallback((type: string, id: string) => {
    if (type === "member") return getMemberName(id);
    if (type === "agent") return getAgentName(id);
    if (type === "squad") return getSquadName(id);
    if (type === "system") return "Multica";
    return "System";
  }, [getAgentName, getMemberName, getSquadName]);

  const getActorInitials = useCallback((type: string, id: string) => {
    const name = getActorName(type, id);
    return name
      .split(" ")
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2);
  }, [getActorName]);

  const getActorAvatarUrl = useCallback((type: string, id: string): string | null => {
    if (type === "member") return members.find((m) => m.user_id === id)?.avatar_url ?? null;
    if (type === "agent") return agents.find((a) => a.id === id)?.avatar_url ?? null;
    if (type === "squad") return squads.find((s) => s.id === id)?.avatar_url ?? null;
    return null;
  }, [agents, members, squads]);

  return useMemo(
    () => ({
      getMemberName,
      getAgentName,
      getSquadName,
      getActorName,
      getActorInitials,
      getActorAvatarUrl,
    }),
    [
      getActorAvatarUrl,
      getActorInitials,
      getActorName,
      getAgentName,
      getMemberName,
      getSquadName,
    ],
  );
}
