"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { FocusListItem } from "@multica/core/types";
import { focusListKeys } from "./queries";

export function useCreateFocusListItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (params: { text: string; issueId?: string | null }) =>
      api.createFocusListItem(params),
    onSuccess: (item) => {
      qc.setQueryData<FocusListItem[]>(focusListKeys.all(wsId), (old) =>
        old ? [...old, item] : [item],
      );
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: focusListKeys.all(wsId) });
    },
  });
}

export function useUpdateFocusListItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...params }: { id: string; text?: string; issueId?: string | null }) =>
      api.updateFocusListItem(id, params),
    onSuccess: (updated) => {
      qc.setQueryData<FocusListItem[]>(focusListKeys.all(wsId), (old) =>
        old?.map((i) => (i.id === updated.id ? updated : i)),
      );
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: focusListKeys.all(wsId) });
    },
  });
}

export function useMarkFocusListItemDone() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markFocusListItemDone(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: focusListKeys.all(wsId) });
      const prev = qc.getQueryData<FocusListItem[]>(focusListKeys.all(wsId));
      qc.setQueryData<FocusListItem[]>(focusListKeys.all(wsId), (old) =>
        old?.map((i) =>
          i.id === id ? { ...i, done_at: new Date().toISOString() } : i,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(focusListKeys.all(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: focusListKeys.all(wsId) });
    },
  });
}

export function useSnoozeFocusListItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, until }: { id: string; until: Date }) =>
      api.snoozeFocusListItem(id, until),
    onMutate: async ({ id, until }) => {
      await qc.cancelQueries({ queryKey: focusListKeys.all(wsId) });
      const prev = qc.getQueryData<FocusListItem[]>(focusListKeys.all(wsId));
      qc.setQueryData<FocusListItem[]>(focusListKeys.all(wsId), (old) =>
        old?.map((i) =>
          i.id === id ? { ...i, snoozed_until: until.toISOString() } : i,
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(focusListKeys.all(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: focusListKeys.all(wsId) });
    },
  });
}

export function useDeleteFocusListItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteFocusListItem(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: focusListKeys.all(wsId) });
      const prev = qc.getQueryData<FocusListItem[]>(focusListKeys.all(wsId));
      qc.setQueryData<FocusListItem[]>(focusListKeys.all(wsId), (old) =>
        old?.filter((i) => i.id !== id),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(focusListKeys.all(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: focusListKeys.all(wsId) });
    },
  });
}
