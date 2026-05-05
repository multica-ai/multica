import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { memoryKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateMemoryArtifactRequest,
  MemoryArtifact,
  UpdateMemoryArtifactRequest,
} from "../types";

// All memory mutations follow the project pattern: optimistic write to
// the detail cache, broad list invalidation on settle. Lists carry
// filter params in their cache keys, so we invalidate the whole `all`
// prefix rather than guessing which filter pages need to refresh.

export function useCreateMemoryArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateMemoryArtifactRequest) =>
      api.createMemoryArtifact(data),
    onSuccess: (created) => {
      // Seed the detail cache so a navigation right after create doesn't
      // refetch. List variants invalidate on settle.
      qc.setQueryData<MemoryArtifact>(
        memoryKeys.detail(wsId, created.id),
        created,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: memoryKeys.all(wsId) });
    },
  });
}

export function useUpdateMemoryArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateMemoryArtifactRequest) =>
      api.updateMemoryArtifact(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: memoryKeys.detail(wsId, id) });
      const prevDetail = qc.getQueryData<MemoryArtifact>(
        memoryKeys.detail(wsId, id),
      );
      qc.setQueryData<MemoryArtifact>(memoryKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data, updated_at: new Date().toISOString() } : old,
      );
      return { prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevDetail) {
        qc.setQueryData(memoryKeys.detail(wsId, ctx.id), ctx.prevDetail);
      }
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: memoryKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: memoryKeys.all(wsId) });
    },
  });
}

// Archive / restore are paired around the archived_at field. The server
// is idempotent on archive but rejects re-archive with 409 — the UI
// can't double-fire because the button only renders for the matching
// state, but the rollback path here covers any race regardless.

export function useArchiveMemoryArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveMemoryArtifact(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: memoryKeys.detail(wsId, id) });
      const prevDetail = qc.getQueryData<MemoryArtifact>(
        memoryKeys.detail(wsId, id),
      );
      qc.setQueryData<MemoryArtifact>(memoryKeys.detail(wsId, id), (old) =>
        old
          ? { ...old, archived_at: new Date().toISOString() }
          : old,
      );
      return { prevDetail, id };
    },
    onError: (_err, id, ctx) => {
      if (ctx?.prevDetail) {
        qc.setQueryData(memoryKeys.detail(wsId, id), ctx.prevDetail);
      }
    },
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: memoryKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: memoryKeys.all(wsId) });
    },
  });
}

export function useRestoreMemoryArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.restoreMemoryArtifact(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: memoryKeys.detail(wsId, id) });
      const prevDetail = qc.getQueryData<MemoryArtifact>(
        memoryKeys.detail(wsId, id),
      );
      qc.setQueryData<MemoryArtifact>(memoryKeys.detail(wsId, id), (old) =>
        old
          ? { ...old, archived_at: null, archived_by: null }
          : old,
      );
      return { prevDetail, id };
    },
    onError: (_err, id, ctx) => {
      if (ctx?.prevDetail) {
        qc.setQueryData(memoryKeys.detail(wsId, id), ctx.prevDetail);
      }
    },
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: memoryKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: memoryKeys.all(wsId) });
    },
  });
}

// Hard delete — admin-only path. The UI surfaces archive as the primary
// destructive action; delete is reserved for explicit "Delete forever"
// affordances on the detail page.
export function useDeleteMemoryArtifact() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteMemoryArtifact(id),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: memoryKeys.detail(wsId, id) });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: memoryKeys.all(wsId) });
    },
  });
}
