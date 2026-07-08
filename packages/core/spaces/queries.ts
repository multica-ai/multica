import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Space } from "../types";

// Canonical presentation order for space lists shown to a user: joined spaces
// first in the personal drag order (mirroring the sidebar's Spaces section),
// then non-joined spaces alphabetically. Pickers and filters share this so
// every space list a user sees is ordered the same way.
export function sortSpacesForDisplay(spaces: Space[]): Space[] {
  const mine = spaces
    .filter((space) => space.is_member)
    .sort((a, b) => a.sort_order - b.sort_order);
  const others = spaces
    .filter((space) => !space.is_member)
    .sort((a, b) => a.name.localeCompare(b.name));
  return [...mine, ...others];
}

export const spaceKeys = {
  all: (wsId: string) => ["spaces", wsId] as const,
  list: (wsId: string) => [...spaceKeys.all(wsId), "list"] as const,
  members: (wsId: string, spaceId: string) =>
    [...spaceKeys.all(wsId), "members", spaceId] as const,
};

export function spaceListOptions(wsId: string) {
  return queryOptions({
    queryKey: spaceKeys.list(wsId),
    queryFn: () => api.listSpaces(),
    select: (data) => data.spaces,
  });
}

export function activeSpaceListOptions(wsId: string) {
  // Shares spaceListOptions' query key (and fetch/cache) — the active/archived
  // distinction is a per-observer `select`, not a separate cache entry, so
  // mutation cache patches on the base key are reflected here too. Sorted
  // with sortSpacesForDisplay so pickers match the sidebar's personal order.
  return queryOptions({
    queryKey: spaceKeys.list(wsId),
    queryFn: () => api.listSpaces(),
    select: (data) =>
      sortSpacesForDisplay(data.spaces.filter((space) => !space.archived_at)),
  });
}

export function spaceMembersOptions(wsId: string, spaceId: string) {
  return queryOptions({
    queryKey: spaceKeys.members(wsId, spaceId),
    queryFn: () => api.listSpaceMembers(spaceId),
    select: (data) => data.members,
  });
}

export function mySpaceListOptions(wsId: string) {
  // The sidebar's Spaces section: only spaces the user joined, in their
  // personal order. Same cache entry as spaceListOptions (per-observer
  // select), so reorder patches on the base key reflect here instantly.
  return queryOptions({
    queryKey: spaceKeys.list(wsId),
    queryFn: () => api.listSpaces(),
    select: (data) =>
      data.spaces
        .filter((space) => space.is_member && !space.archived_at)
        .sort((a, b) => a.sort_order - b.sort_order),
  });
}
