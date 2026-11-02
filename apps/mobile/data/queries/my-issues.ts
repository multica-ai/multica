/**
 * Server-filtered My Issues scopes: direct assignments, creations, or involvement
 * through owned agents and squad membership. The agents scope excludes direct
 * member assignment, which belongs to assigned.
 *
 * Mobile owns these query keys and its QueryClient. useMyIssuesRealtime patches
 * or invalidates local lists; each client receives its own server events.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import {
  issueKeys,
  type MyIssuesFilter,
  type MyIssuesScope,
} from "./issue-keys";

export function buildMyIssuesFilter(
  scope: MyIssuesScope,
  userId: string,
): MyIssuesFilter {
  switch (scope) {
    case "assigned":
      return { assignee_id: userId };
    case "created":
      return { creator_id: userId };
    case "agents":
      return { involves_user_id: userId };
  }
}

export const myIssueListOptions = (
  wsId: string | null,
  scope: MyIssuesScope,
  filter: MyIssuesFilter,
) =>
  queryOptions({
    queryKey: issueKeys.myList(wsId, scope, filter),
    queryFn: async ({ signal }) => {
      const res = await api.listIssues(filter, { signal });
      return res.issues;
    },
    enabled: !!wsId,
  });
