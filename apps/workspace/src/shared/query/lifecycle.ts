import type { Query, QueryClient } from "@tanstack/react-query";
import { isWorkspaceScopedQueryKey, queryKeyIncludesWorkspace, queryKeys } from "./keys";

function matchesWorkspaceScope(query: Query, workspaceId?: string | null): boolean {
  if (!isWorkspaceScopedQueryKey(query.queryKey)) {
    return false;
  }

  if (!workspaceId) {
    return true;
  }

  const root = query.queryKey[0];
  if (root === "issues" || root === "tasks") {
    return true;
  }

  return queryKeyIncludesWorkspace(query.queryKey, workspaceId);
}

export async function prepareQueryCacheForLogin(queryClient: QueryClient) {
  await queryClient.cancelQueries();
  queryClient.clear();
}

export async function prepareQueryCacheForLogout(queryClient: QueryClient) {
  await queryClient.cancelQueries();
  queryClient.clear();
}

export async function clearWorkspaceScopedQueryCaches(
  queryClient: QueryClient,
  workspaceId?: string | null,
) {
  await queryClient.cancelQueries({
    predicate: (query) => matchesWorkspaceScope(query, workspaceId),
  });

  queryClient.removeQueries({
    predicate: (query) => matchesWorkspaceScope(query, workspaceId),
  });
}

export async function prepareQueryCacheForWorkspaceSwitch(
  queryClient: QueryClient,
  previousWorkspaceId?: string | null,
) {
  await clearWorkspaceScopedQueryCaches(queryClient, previousWorkspaceId);
  await queryClient.invalidateQueries({ queryKey: queryKeys.workspaces.all() });
}

export async function prepareQueryCacheForReconnect(
  queryClient: QueryClient,
  workspaceId?: string | null,
) {
  await queryClient.invalidateQueries({ queryKey: queryKeys.session.all() });
  await queryClient.invalidateQueries({ queryKey: queryKeys.workspaces.all() });
  await queryClient.invalidateQueries({
    predicate: (query) => matchesWorkspaceScope(query, workspaceId),
  });
}
