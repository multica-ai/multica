"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "../auth";
import { useWorkspaceId } from "../hooks";
import {
  buildWorkspaceMentionTargets,
  sortMentionTargetsByFrequency,
  type WorkspaceMentionTarget,
} from "./mentions";
import {
  memberListOptions,
  agentListOptions,
  squadListOptions,
  mentionFrequencyOptions,
  workspaceListOptions,
} from "./queries";

export function useWorkspaceList() {
  return useQuery(workspaceListOptions());
}

export function useMemberList(workspaceId: string) {
  return useQuery(memberListOptions(workspaceId));
}

export function useAgentList(workspaceId: string) {
  return useQuery(agentListOptions(workspaceId));
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

export type { WorkspaceMentionTarget } from "./mentions";

export function useWorkspaceMentionTargets(wsId: string) {
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: mentionFrequency = [] } = useQuery(mentionFrequencyOptions(wsId));
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const role = useMemo(
    () => members.find((member) => member.user_id === userId)?.role ?? null,
    [members, userId],
  );

  return useMemo<WorkspaceMentionTarget[]>(
    () => {
      const targets = buildWorkspaceMentionTargets(
        members,
        agents,
        { userId, role },
        squads,
      );
      const ownAgentIds = agents
        .filter((agent) => userId !== null && agent.owner_id === userId)
        .map((agent) => agent.id);
      return sortMentionTargetsByFrequency(targets, mentionFrequency, {
        ownAgentIds,
      });
    },
    [agents, members, mentionFrequency, role, squads, userId],
  );
}
