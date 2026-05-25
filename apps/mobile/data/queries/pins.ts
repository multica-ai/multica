/**
 * Pin cache key factory. Mirrors web `packages/core/pins/queries.ts` —
 * `["pins", wsId, userId, "list"]`. The userId segment matters: pin lists are
 * per-user-per-workspace (each member curates their own sidebar pins), so
 * keying only on wsId would mix two users on a shared device.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const pinKeys = {
  all: (wsId: string | null, userId: string | null) =>
    ["pins", wsId, userId] as const,
  list: (wsId: string | null, userId: string | null) =>
    [...pinKeys.all(wsId, userId), "list"] as const,
};

export const pinListOptions = (wsId: string | null, userId: string | null) =>
  queryOptions({
    queryKey: pinKeys.list(wsId, userId),
    queryFn: ({ signal }) => api.listPins({ signal }),
    enabled: !!wsId && !!userId,
  });
