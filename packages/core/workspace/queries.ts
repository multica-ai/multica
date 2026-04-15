import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const workspaceKeys = {
  all: (wsId: string) => ["workspaces", wsId] as const,
  list: () => ["workspaces", "list"] as const,
  members: (wsId: string) => ["workspaces", wsId, "members"] as const,
  invitations: (wsId: string) => ["workspaces", wsId, "invitations"] as const,
  myInvitations: () => ["invitations", "mine"] as const,
  agents: (wsId: string) => ["workspaces", wsId, "agents"] as const,
  skills: (wsId: string) => ["workspaces", wsId, "skills"] as const,
  assigneeFrequency: (wsId: string) => ["workspaces", wsId, "assignee-frequency"] as const,
};

export function workspaceListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.list(),
    queryFn: () => api.listWorkspaces(),
  });
}

export function memberListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.members(wsId),
    queryFn: () => api.listMembers(wsId),
  });
}

export function agentListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.agents(wsId),
    queryFn: () =>
      api.listAgents({ workspace_id: wsId, include_archived: true }),
  });
}

export function skillListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.skills(wsId),
    queryFn: () => api.listSkills(),
  });
}

export function invitationListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.invitations(wsId),
    queryFn: () => api.listWorkspaceInvitations(wsId),
  });
}

export function myInvitationListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.myInvitations(),
    queryFn: () => api.listMyInvitations(),
  });
}

export function assigneeFrequencyOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.assigneeFrequency(wsId),
    queryFn: () => api.getAssigneeFrequency(),
  });
}
