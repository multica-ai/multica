import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type { ListProjectFilesResponse } from "../types";

export const projectFileKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "files"] as const,
};

export function projectFilesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectFileKeys.list(wsId, projectId),
    queryFn: () => api.listProjectFiles(projectId),
    select: (data) => data.files,
  });
}

/**
 * Optimistic hide/unhide helpers. Both flip only the `hidden` flag on the
 * matching file in the cached list; the server response is a bare 204, so the
 * cache patch is the whole client-side story until the settle invalidation
 * re-fetches. Rollback is trivial (swap the previous list back), which is what
 * the optimistic-update contract requires.
 */
function patchHidden(qc: ReturnType<typeof useQueryClient>) {
  return (wsId: string, projectId: string, attachmentId: string, hidden: boolean) => {
    qc.setQueryData<ListProjectFilesResponse>(
      projectFileKeys.list(wsId, projectId),
      (old) =>
        old
          ? {
              ...old,
              files: old.files.map((f) =>
                f.id === attachmentId ? { ...f, hidden } : f,
              ),
            }
          : old,
    );
  };
}

export function useHideProjectFile(wsId: string, projectId: string) {
  const qc = useQueryClient();
  const patch = patchHidden(qc);
  return useMutation({
    mutationFn: (attachmentId: string) =>
      api.hideProjectFile(projectId, attachmentId),
    onMutate: async (attachmentId) => {
      await qc.cancelQueries({ queryKey: projectFileKeys.list(wsId, projectId) });
      const prev = qc.getQueryData<ListProjectFilesResponse>(
        projectFileKeys.list(wsId, projectId),
      );
      patch(wsId, projectId, attachmentId, true);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(projectFileKeys.list(wsId, projectId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectFileKeys.list(wsId, projectId) });
    },
  });
}

export function useUnhideProjectFile(wsId: string, projectId: string) {
  const qc = useQueryClient();
  const patch = patchHidden(qc);
  return useMutation({
    mutationFn: (attachmentId: string) =>
      api.unhideProjectFile(projectId, attachmentId),
    onMutate: async (attachmentId) => {
      await qc.cancelQueries({ queryKey: projectFileKeys.list(wsId, projectId) });
      const prev = qc.getQueryData<ListProjectFilesResponse>(
        projectFileKeys.list(wsId, projectId),
      );
      patch(wsId, projectId, attachmentId, false);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(projectFileKeys.list(wsId, projectId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectFileKeys.list(wsId, projectId) });
    },
  });
}
