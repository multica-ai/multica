import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { viewKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  SavedView,
  ListViewsResponse,
  ViewPage,
  CreateViewRequest,
  UpdateViewRequest,
} from "../types";

export function useCreateView(page: ViewPage, projectId?: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const key = viewKeys.list(wsId, page, projectId);
  return useMutation({
    mutationFn: (data: CreateViewRequest) => api.createView(data),
    onSuccess: (view) => {
      qc.setQueryData<ListViewsResponse>(key, (old) =>
        old && !old.views.some((v) => v.id === view.id)
          ? { views: [...old.views, view] }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}

export function useUpdateView(page: ViewPage, projectId?: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const key = viewKeys.list(wsId, page, projectId);
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateViewRequest) =>
      api.updateView(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ListViewsResponse>(key);
      qc.setQueryData<ListViewsResponse>(key, (old) =>
        old
          ? { views: old.views.map((v) => (v.id === id ? { ...v, ...data } : v)) }
          : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}

export function useDeleteView(page: ViewPage, projectId?: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const key = viewKeys.list(wsId, page, projectId);
  return useMutation({
    mutationFn: (id: string) => api.deleteView(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ListViewsResponse>(key);
      qc.setQueryData<ListViewsResponse>(key, (old) =>
        old ? { views: old.views.filter((v) => v.id !== id) } : old,
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}

export function useReorderViews(page: ViewPage, projectId?: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const key = viewKeys.list(wsId, page, projectId);
  return useMutation({
    mutationFn: (ids: string[]) => api.reorderViews({ ids }),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<ListViewsResponse>(key);
      qc.setQueryData<ListViewsResponse>(key, (old) => {
        if (!old) return old;
        const byId = new Map(old.views.map((v) => [v.id, v]));
        const ordered = ids
          .map((id, i) => {
            const v = byId.get(id);
            return v ? { ...v, position: i } : undefined;
          })
          .filter((v): v is SavedView => v !== undefined);
        // Append any views not named in `ids` so none silently disappear.
        const named = new Set(ids);
        const rest = old.views.filter((v) => !named.has(v.id));
        return { views: [...ordered, ...rest] };
      });
      return { prev };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}
