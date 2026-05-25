import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/**
 * Inbox cache key factory.
 *
 * Shape mirrors web's `packages/core/inbox/queries.ts` — `["inbox", wsId, "list"]`
 * — so cross-platform mental model stays the same. Keying on wsId means
 * workspace switches naturally invalidate (TQ sees a new key and refetches).
 */
export const inboxKeys = {
  all: (wsId: string | null) => ["inbox", wsId] as const,
  list: (wsId: string | null) =>
    [...inboxKeys.all(wsId), "list"] as const,
};

export const inboxListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: ({ signal }) => api.listInbox({ signal }),
    enabled: !!wsId,
  });
