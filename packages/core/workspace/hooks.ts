"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "../hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "./queries";
import { resolvePublicFileUrl } from "./avatar-url";

/**
 * Pure actor-name resolution over explicit directory snapshots. Async flows
 * (e.g. CSV export) must resolve names from directories they have awaited
 * themselves — a hook-bound getActorName closes over whatever the queries
 * held at render time, silently naming everyone "Unknown*" while the
 * directories are still loading.
 */
export function buildActorNameResolver(directories: {
  members: readonly { user_id: string; name: string }[];
  agents: readonly { id: string; name: string }[];
  squads: readonly { id: string; name: string }[];
}) {
  const memberNames = new Map(directories.members.map((m) => [m.user_id, m.name]));
  const agentNames = new Map(directories.agents.map((a) => [a.id, a.name]));
  const squadNames = new Map(directories.squads.map((s) => [s.id, s.name]));
  return (type: string, id: string) => {
    if (type === "member") return memberNames.get(id) ?? "Unknown";
    if (type === "agent") return agentNames.get(id) ?? "Unknown Agent";
    if (type === "squad") return squadNames.get(id) ?? "Unknown Squad";
    if (type === "system") return "Multica";
    return "System";
  };
}

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

  const getActorName = useMemo(
    () => buildActorNameResolver({ members, agents, squads }),
    [agents, members, squads],
  );

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
    if (type === "member") return resolvePublicFileUrl(members.find((m) => m.user_id === id)?.avatar_url);
    if (type === "agent") return resolvePublicFileUrl(agents.find((a) => a.id === id)?.avatar_url);
    if (type === "squad") return resolvePublicFileUrl(squads.find((s) => s.id === id)?.avatar_url);
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
