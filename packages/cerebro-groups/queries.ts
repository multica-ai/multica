import { queryOptions } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import {
  cerebroGroupListSchema,
  cerebroGroupMemberListSchema,
  type CerebroGroup,
  type CerebroGroupMember,
} from "./types";

export const groupKeys = {
  all: (wsId: string) => ["cerebro", "groups", wsId] as const,
  list: (wsId: string) => ["cerebro", "groups", wsId, "list"] as const,
  members: (wsId: string, groupId: string) =>
    ["cerebro", "groups", wsId, "members", groupId] as const,
};

const EMPTY_GROUPS: CerebroGroup[] = [];
const EMPTY_MEMBERS: CerebroGroupMember[] = [];

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
