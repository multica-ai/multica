import { queryOptions } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import {
  cerebroGroupListSchema,
  cerebroGroupMemberListSchema,
  cerebroGroupCapabilityListSchema,
  cerebroGroupRuntimeAccessListSchema,
  cerebroGroupAgentAccessListSchema,
  cerebroProjectGroupMemberListSchema,
  type CerebroGroup,
  type CerebroGroupMember,
  type CerebroGroupCapabilityRow,
  type CerebroGroupRuntimeAccess,
  type CerebroGroupAgentAccess,
  type CerebroProjectGroupMember,
} from "./types";

export const groupKeys = {
  all: (wsId: string) => ["cerebro", "groups", wsId] as const,
  list: (wsId: string) => ["cerebro", "groups", wsId, "list"] as const,
  members: (wsId: string, groupId: string) =>
    ["cerebro", "groups", wsId, "members", groupId] as const,
  capabilities: (wsId: string, groupId: string) =>
    ["cerebro", "groups", wsId, "capabilities", groupId] as const,
  runtimes: (wsId: string, groupId: string) =>
    ["cerebro", "groups", wsId, "runtimes", groupId] as const,
  agents: (wsId: string, groupId: string) =>
    ["cerebro", "groups", wsId, "agents", groupId] as const,
  projectGroups: (wsId: string, projectId: string) =>
    ["cerebro", "projects", wsId, "group-access", projectId] as const,
};

const EMPTY_GROUPS: CerebroGroup[] = [];
const EMPTY_MEMBERS: CerebroGroupMember[] = [];
const EMPTY_CAPS: CerebroGroupCapabilityRow[] = [];
const EMPTY_RUNTIME_ACCESS: CerebroGroupRuntimeAccess[] = [];
const EMPTY_AGENT_ACCESS: CerebroGroupAgentAccess[] = [];
const EMPTY_PROJECT_GROUPS: CerebroProjectGroupMember[] = [];

export function groupListOptions(wsId: string) {
  return queryOptions({
    queryKey: groupKeys.list(wsId),
    queryFn: async (): Promise<CerebroGroup[]> => {
      const raw = await api.listCerebroGroups<unknown>(wsId);
      return parseWithFallback(raw, cerebroGroupListSchema, EMPTY_GROUPS, {
        endpoint: "listCerebroGroups",
      });
    },
    enabled: !!wsId,
  });
}

export function groupMembersOptions(wsId: string, groupId: string) {
  return queryOptions({
    queryKey: groupKeys.members(wsId, groupId),
    queryFn: async (): Promise<CerebroGroupMember[]> => {
      const raw = await api.listCerebroGroupMembers<unknown>(groupId);
      return parseWithFallback(
        raw,
        cerebroGroupMemberListSchema,
        EMPTY_MEMBERS,
        { endpoint: "listCerebroGroupMembers" },
      );
    },
    enabled: !!wsId && !!groupId,
  });
}

export function groupCapabilitiesOptions(wsId: string, groupId: string) {
  return queryOptions({
    queryKey: groupKeys.capabilities(wsId, groupId),
    queryFn: async (): Promise<CerebroGroupCapabilityRow[]> => {
      const raw = await api.listCerebroGroupCapabilities<unknown>(groupId);
      return parseWithFallback(
        raw,
        cerebroGroupCapabilityListSchema,
        EMPTY_CAPS,
        { endpoint: "listCerebroGroupCapabilities" },
      );
    },
    enabled: !!wsId && !!groupId,
  });
}

export function groupRuntimesOptions(wsId: string, groupId: string) {
  return queryOptions({
    queryKey: groupKeys.runtimes(wsId, groupId),
    queryFn: async (): Promise<CerebroGroupRuntimeAccess[]> => {
      const raw = await api.listCerebroGroupRuntimes<unknown>(groupId);
      return parseWithFallback(
        raw,
        cerebroGroupRuntimeAccessListSchema,
        EMPTY_RUNTIME_ACCESS,
        { endpoint: "listCerebroGroupRuntimes" },
      );
    },
    enabled: !!wsId && !!groupId,
  });
}

export function groupAgentsOptions(wsId: string, groupId: string) {
  return queryOptions({
    queryKey: groupKeys.agents(wsId, groupId),
    queryFn: async (): Promise<CerebroGroupAgentAccess[]> => {
      const raw = await api.listCerebroGroupAgents<unknown>(groupId);
      return parseWithFallback(
        raw,
        cerebroGroupAgentAccessListSchema,
        EMPTY_AGENT_ACCESS,
        { endpoint: "listCerebroGroupAgents" },
      );
    },
    enabled: !!wsId && !!groupId,
  });
}

export function projectGroupsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: groupKeys.projectGroups(wsId, projectId),
    queryFn: async (): Promise<CerebroProjectGroupMember[]> => {
      const raw = await api.listCerebroProjectGroups<unknown>(projectId);
      return parseWithFallback(
        raw,
        cerebroProjectGroupMemberListSchema,
        EMPTY_PROJECT_GROUPS,
        { endpoint: "listCerebroProjectGroups" },
      );
    },
    enabled: !!wsId && !!projectId,
  });
}
