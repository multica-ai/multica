"use client";

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useWorkspaceId } from "../hooks";
import { memberListOptions, agentListOptions, workspaceListOptions } from "./queries";

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

  const getMemberName = (userId: string) => {
    const m = members.find((m) => m.user_id === userId);
    return m?.name ?? "Unknown";
  };

  const getAgentName = (agentId: string) => {
    const a = agents.find((a) => a.id === agentId);
    return a?.name ?? "Unknown Agent";
  };

  const getActorName = (type: string, id: string) => {
    if (type === "member") return getMemberName(id);
    if (type === "agent") return getAgentName(id);
    return "System";
  };

  const getActorInitials = (type: string, id: string) => {
    const name = getActorName(type, id);
    return name
      .split(" ")
      .map((w) => w[0])
      .join("")
      .toUpperCase()
      .slice(0, 2);
  };

  const getActorAvatarUrl = (type: string, id: string): string | null => {
    if (type === "member") return members.find((m) => m.user_id === id)?.avatar_url ?? null;
    if (type === "agent") return agents.find((a) => a.id === id)?.avatar_url ?? null;
    return null;
  };

  return { getMemberName, getAgentName, getActorName, getActorInitials, getActorAvatarUrl };
}

export type WorkspaceMentionTarget = {
  id: string;
  label: string;
  type: "all" | "member" | "agent";
};

export function useWorkspaceMentionTargets(wsId: string) {
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  return useMemo<WorkspaceMentionTarget[]>(
    () => [
      { id: "all", label: "All members", type: "all" },
      ...members.map((member) => ({
        id: member.user_id,
        label: member.name,
        type: "member" as const,
      })),
      ...agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => ({
          id: agent.id,
          label: agent.name,
          type: "agent" as const,
        })),
    ],
    [agents, members],
  );
}
