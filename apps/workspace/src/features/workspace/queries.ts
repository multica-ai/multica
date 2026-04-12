import { useQuery } from "@tanstack/react-query";
import type { Agent, MemberWithUser, Skill, Workspace } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";

const WORKSPACE_STALE_TIME = 60 * 1000;
const EMPTY_WORKSPACE_KEY = "__no_workspace__";

export function workspacesQueryOptions() {
  return {
    queryKey: queryKeys.workspaces.all(),
    queryFn: () => api.listWorkspaces(),
    staleTime: WORKSPACE_STALE_TIME,
  };
}

export function workspaceMembersQueryOptions(workspaceId: string) {
  return {
    queryKey: queryKeys.workspace.members(workspaceId),
    queryFn: () => api.listMembers(workspaceId),
    staleTime: WORKSPACE_STALE_TIME,
  };
}

export function workspaceAgentsQueryOptions(workspaceId: string) {
  return {
    queryKey: queryKeys.workspace.agents(workspaceId),
    queryFn: () => api.listAgents({ workspace_id: workspaceId, include_archived: true }),
    staleTime: WORKSPACE_STALE_TIME,
  };
}

export function workspaceSkillsQueryOptions(workspaceId: string) {
  return {
    queryKey: queryKeys.workspace.skills(workspaceId),
    queryFn: () => api.listSkills(),
    staleTime: WORKSPACE_STALE_TIME,
  };
}

export function useWorkspacesQuery() {
  return useQuery<Workspace[]>({
    ...workspacesQueryOptions(),
    enabled: hasStoredSessionToken(),
  });
}

export function useWorkspaceMembersQuery(workspaceId?: string | null) {
  return useQuery<MemberWithUser[]>({
    ...(workspaceId
      ? workspaceMembersQueryOptions(workspaceId)
      : {
          queryKey: queryKeys.workspace.members(EMPTY_WORKSPACE_KEY),
          queryFn: async () => [] as MemberWithUser[],
          staleTime: WORKSPACE_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}

export function useWorkspaceAgentsQuery(workspaceId?: string | null) {
  return useQuery<Agent[]>({
    ...(workspaceId
      ? workspaceAgentsQueryOptions(workspaceId)
      : {
          queryKey: queryKeys.workspace.agents(EMPTY_WORKSPACE_KEY),
          queryFn: async () => [] as Agent[],
          staleTime: WORKSPACE_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}

export function useWorkspaceSkillsQuery(workspaceId?: string | null) {
  return useQuery<Skill[]>({
    ...(workspaceId
      ? workspaceSkillsQueryOptions(workspaceId)
      : {
          queryKey: queryKeys.workspace.skills(EMPTY_WORKSPACE_KEY),
          queryFn: async () => [] as Skill[],
          staleTime: WORKSPACE_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}
