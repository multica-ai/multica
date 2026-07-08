import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import { autopilotKeys } from "../autopilots/queries";
import { spaceKeys } from "./queries";
import type { CreateSpaceRequest, ListSpacesResponse, UpdateSpaceRequest } from "../types";

export function useCreateSpace() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateSpaceRequest) => api.createSpace(data),
    // Create stays onSuccess-append: there's no stable local id to seed an
    // optimistic row, so we wait for the server-assigned space.
    onSuccess: (space) => {
      qc.setQueryData<ListSpacesResponse>(spaceKeys.list(wsId), (old) =>
        old && !old.spaces.some((t) => t.id === space.id)
          ? { ...old, spaces: [...old.spaces, space], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
    },
  });
}

export function useUpdateSpace() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateSpaceRequest) =>
      api.updateSpace(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: spaceKeys.list(wsId) });
      const prevList = qc.getQueryData<ListSpacesResponse>(spaceKeys.list(wsId));
      qc.setQueryData<ListSpacesResponse>(spaceKeys.list(wsId), (old) =>
        old
          ? { ...old, spaces: old.spaces.map((t) => (t.id === id ? { ...t, ...data } : t)) }
          : old,
      );
      return { prevList };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(spaceKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
      // A key change re-derives every issue identifier under the space.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useUpdateSpaceMembership() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, sort_order }: { id: string; sort_order: number }) =>
      api.updateSpaceMembership(id, { sort_order }),
    // Optimistic: sidebar ordering must not snap back while the PATCH is in
    // flight. Fractional sort keys mean only the dragged space's row changes.
    onMutate: async ({ id, sort_order }) => {
      await qc.cancelQueries({ queryKey: spaceKeys.list(wsId) });
      const prevList = qc.getQueryData<ListSpacesResponse>(spaceKeys.list(wsId));
      qc.setQueryData<ListSpacesResponse>(spaceKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              spaces: old.spaces.map((t) => (t.id === id ? { ...t, sort_order } : t)),
            }
          : old,
      );
      return { prevList };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(spaceKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
    },
  });
}

export function useReplaceSpaceMembers() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, member_ids }: { id: string; member_ids: string[] }) =>
      api.replaceSpaceMembers(id, member_ids),
    // No optimistic patch: the config panel closes on save, and the
    // caller's own is_member may flip (they can add/remove themselves), so
    // one settled invalidation of the list + members caches is the simplest
    // correct reconcile.
    onSettled: (_data, _err, { id }) => {
      qc.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: spaceKeys.members(wsId, id) });
    },
  });
}

export function useArchiveSpace() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveSpace(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: spaceKeys.list(wsId) });
      const prevList = qc.getQueryData<ListSpacesResponse>(spaceKeys.list(wsId));
      const archivedAt = new Date().toISOString();
      qc.setQueryData<ListSpacesResponse>(spaceKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              spaces: old.spaces.map((t) =>
                t.id === id ? { ...t, archived_at: archivedAt } : t,
              ),
            }
          : old,
      );
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(spaceKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    },
  });
}
