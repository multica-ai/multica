import { QueryClient } from "@tanstack/react-query";
import { setWorkspaceSlugLookup } from "./paths/workspace-slugs";
import { workspaceKeys } from "./workspace/queries";
import type { Workspace } from "./types";

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: Infinity,
        gcTime: 10 * 60 * 1000, // 10 minutes
        refetchOnWindowFocus: false,
        refetchOnReconnect: true,
        retry: 1,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

/**
 * Point path resolution at `client`'s workspace list. Reading the cache on
 * every lookup covers fetches, hydration, mutations and logout/clear. Owned by
 * the mounted provider rather than the constructor: StrictMode builds and
 * discards an extra client, which must never become the bound one.
 */
export function bindWorkspaceSlugLookup(client: QueryClient): () => void {
  return setWorkspaceSlugLookup(
    (slug) =>
      client
        .getQueryData<Workspace[]>(workspaceKeys.list())
        ?.some((workspace) => workspace.slug === slug) ?? false,
  );
}
