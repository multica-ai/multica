import { QueryClient } from "@tanstack/react-query";
import { inboxQueryRetry, inboxQueryRetryDelay } from "./inbox/retry";
import { setWorkspaceSlugLookup } from "./paths/workspace-slugs";
import { workspaceKeys } from "./workspace/queries";
import type { Workspace } from "./types";

export function createQueryClient(): QueryClient {
  const client = new QueryClient({
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
  // Keep the policy on the app client so custom clients and explicit query
  // overrides retain their retry settings. A busy server (503 + Retry-After)
  // is tried again a few times with backoff instead of leaving the Inbox
  // looking empty (MUL-9635); see ./inbox/retry.
  client.setQueryDefaults(["inbox"], {
    retry: inboxQueryRetry,
    retryDelay: (failureCount, error) => inboxQueryRetryDelay(failureCount, error),
  });
  return client;
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
