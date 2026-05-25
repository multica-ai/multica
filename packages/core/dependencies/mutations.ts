// CEREBRO-PATCH(issue-dependencies): add/remove mutations for issue relations.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { dependencyKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import type { IssueDependenciesResponse } from "../types";

/**
 * The add/remove endpoints all return the full grouped dependencies object, so
 * each mutation seeds the dependencies cache from the response and then
 * invalidates: the dependencies query (canonical source) and the issues scope
 * (issues now embed blocks/blocked_by snapshots that lists/board render). No
 * optimistic update — relation edits are infrequent and the server response is
 * authoritative.
 */
function useDependencyMutation(
  issueId: string,
  call: (issueId: string, otherId: string) => Promise<IssueDependenciesResponse>,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (otherId: string) => call(issueId, otherId),
    onSuccess: (data) => {
      qc.setQueryData<IssueDependenciesResponse>(
        dependencyKeys.byIssue(wsId, issueId),
        data,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: dependencyKeys.byIssue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useAddBlocks(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.addBlocks(id, otherId));
}

export function useAddBlockedBy(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.addBlockedBy(id, otherId));
}

export function useAddRelated(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.addRelated(id, otherId));
}

export function useRemoveBlocks(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.removeBlocks(id, otherId));
}

export function useRemoveBlockedBy(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.removeBlockedBy(id, otherId));
}

export function useRemoveRelated(issueId: string) {
  return useDependencyMutation(issueId, (id, otherId) => api.removeRelated(id, otherId));
}
