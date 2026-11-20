import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { useFoldersT } from "./i18n";

export interface ProjectFolder {
  id: string;
  name: string;
}

export interface FiledProject {
  project_id: string;
  folder_id: string;
}

export interface ProjectFolderList {
  folders: ProjectFolder[];
  /** Where each project the caller can see is filed. */
  filed: FiledProject[];
}

const FolderSchema = z.object({ id: z.string(), name: z.string() }).loose();

const ListSchema = z
  .object({
    folders: z.array(FolderSchema).default([]).catch([]),
    filed: z.array(z.object({ project_id: z.string(), folder_id: z.string() })).default([]).catch([]),
  })
  .loose();

const EMPTY_LIST: ProjectFolderList = { folders: [], filed: [] };

const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });

export const folderApi = {
  async list(): Promise<ProjectFolderList> {
    return parseWithFallback(await api.request("/api/project-folders"), ListSchema, EMPTY_LIST, {
      endpoint: "GET /api/project-folders",
    });
  },
  async create(name: string): Promise<ProjectFolder> {
    return FolderSchema.parse(await api.request("/api/project-folders", json("POST", { name })));
  },
  async rename(id: string, name: string): Promise<ProjectFolder> {
    return FolderSchema.parse(await api.request(`/api/project-folders/${id}`, json("PATCH", { name })));
  },
  async remove(id: string): Promise<void> {
    await api.request(`/api/project-folders/${id}`, { method: "DELETE" });
  },
  async file(projectId: string, folderId: string | null): Promise<void> {
    await api.request("/api/project-folders/file", json("POST", { project_id: projectId, folder_id: folderId }));
  },
};

export const folderKeys = {
  list: (wsId: string) => ["ee", "project-folders", wsId] as const,
};

export function projectFolderListOptions(wsId: string) {
  return queryOptions({
    queryKey: folderKeys.list(wsId),
    queryFn: () => folderApi.list(),
    enabled: wsId.length > 0,
  });
}

function errorText(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

export function useCreateFolder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => folderApi.create(name),
    onSuccess: (folder) => {
      qc.setQueryData<ProjectFolderList>(folderKeys.list(wsId), (old) =>
        old && !old.folders.some((f) => f.id === folder.id) ? { ...old, folders: [...old.folders, folder] } : old,
      );
    },
    onSettled: () => qc.invalidateQueries({ queryKey: folderKeys.list(wsId) }),
  });
}

export function useRenameFolder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => folderApi.rename(id, name),
    onMutate: async ({ id, name }) => {
      await qc.cancelQueries({ queryKey: folderKeys.list(wsId) });
      const prev = qc.getQueryData<ProjectFolderList>(folderKeys.list(wsId));
      qc.setQueryData<ProjectFolderList>(folderKeys.list(wsId), (old) =>
        old ? { ...old, folders: old.folders.map((f) => (f.id === id ? { ...f, name } : f)) } : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(folderKeys.list(wsId), ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: folderKeys.list(wsId) }),
  });
}

/** Deleting a folder keeps its projects; the server moves them to the top level. */
export function useDeleteFolder(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => folderApi.remove(id),
    onSuccess: (_data, id) => {
      qc.setQueryData<ProjectFolderList>(folderKeys.list(wsId), (old) =>
        old
          ? { folders: old.folders.filter((f) => f.id !== id), filed: old.filed.filter((p) => p.folder_id !== id) }
          : old,
      );
    },
    onSettled: () => qc.invalidateQueries({ queryKey: folderKeys.list(wsId) }),
  });
}

/**
 * Files a project in a folder, or moves it out with null. Failures are
 * reported here, so they show even when the caller has closed (the new-project
 * dialog files after it closes).
 */
export function useFileProject(wsId: string) {
  const qc = useQueryClient();
  const t = useFoldersT();
  return useMutation({
    mutationFn: ({ projectId, folderId }: { projectId: string; folderId: string | null }) =>
      folderApi.file(projectId, folderId),
    onMutate: async ({ projectId, folderId }) => {
      await qc.cancelQueries({ queryKey: folderKeys.list(wsId) });
      const prev = qc.getQueryData<ProjectFolderList>(folderKeys.list(wsId));
      qc.setQueryData<ProjectFolderList>(folderKeys.list(wsId), (old) => {
        if (!old) return old;
        const filed = old.filed.filter((p) => p.project_id !== projectId);
        return { ...old, filed: folderId ? [...filed, { project_id: projectId, folder_id: folderId }] : filed };
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(folderKeys.list(wsId), ctx.prev);
      toast.error(errorText(err, t("toast_move_failed")));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: folderKeys.list(wsId) }),
  });
}
