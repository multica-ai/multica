import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { quickActionKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateQuickActionRequest,
  UpdateQuickActionRequest,
  ListQuickActionsResponse,
} from "../types";

export function useCreateQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateQuickActionRequest) => api.createQuickAction(data),
    onSuccess: (action) => {
      qc.setQueryData<ListQuickActionsResponse>(quickActionKeys.list(wsId), (old) =>
        old && !old.quick_actions.some((a) => a.id === action.id)
          ? { ...old, quick_actions: [...old.quick_actions, action], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.list(wsId) });
    },
  });
}

/**
 * Optimistic edit: apply locally, snapshot for rollback, invalidate on settle —
 * so the manager dialog doesn't freeze for the round-trip on every keystroke-save.
 */
export function useUpdateQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateQuickActionRequest) =>
      api.updateQuickAction(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: quickActionKeys.list(wsId) });
      const prev = qc.getQueryData<ListQuickActionsResponse>(quickActionKeys.list(wsId));
      qc.setQueryData<ListQuickActionsResponse>(quickActionKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              quick_actions: old.quick_actions.map((a) =>
                a.id === id ? { ...a, ...data } : a,
              ),
            }
          : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(quickActionKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.list(wsId) });
    },
  });
}

export function useDeleteQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteQuickAction(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: quickActionKeys.list(wsId) });
      const prev = qc.getQueryData<ListQuickActionsResponse>(quickActionKeys.list(wsId));
      qc.setQueryData<ListQuickActionsResponse>(quickActionKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              quick_actions: old.quick_actions.filter((a) => a.id !== id),
              total: old.total - 1,
            }
          : old,
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(quickActionKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.list(wsId) });
    },
  });
}
