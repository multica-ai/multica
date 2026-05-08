"use client";

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
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

export type { WorkspaceMentionTarget } from "./mentions";

export function useWorkspaceMentionTargets(wsId: string) {
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
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
      );
      const all = targets.find((target) => target.type === "all");
      const mentionable = targets.filter((target) => target.type !== "all");
      return [
        ...(all ? [all] : []),
        ...sortMentionTargetsByFrequency(mentionable, mentionFrequency),
      ];
    },
    [agents, members, mentionFrequency, role, userId],
  );
}
