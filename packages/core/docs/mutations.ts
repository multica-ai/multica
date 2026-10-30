import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { CreateDocFolderRequest, CreateDocRequest, Doc, DocTree, UpdateDocRequest } from "../types";
import { docKeys } from "./queries";

/**
 * Knowledge base mutations (MUL-8370). Every write also publishes a realtime
 * event that refreshes `docs/<wsId>` for all clients, the writer included, so
 * these hooks only seed the caches the writer is about to render.
 */

function useDocTreeCache() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const update = (fn: (tree: DocTree) => DocTree) =>
    qc.setQueryData<DocTree>(docKeys.tree(wsId), (old) => (old ? fn(old) : old));
  return { qc, wsId, update };
}

export function useCreateDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (data: CreateDocRequest) => api.createDoc(data),
    onSuccess: (doc) => {
      qc.setQueryData(docKeys.detail(wsId, doc.id), doc);
      update((tree) => ({ ...tree, docs: [...tree.docs.filter((d) => d.id !== doc.id), doc] }));
    },
  });
}

/**
 * Saves a doc. Callers handle `isDocRevisionConflict` errors themselves (the
 * editor merges and retries), so a conflict is not treated as a failure here.
 */
export function useUpdateDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateDocRequest) => api.updateDoc(id, data),
    onSuccess: (doc) => {
      qc.setQueryData<Doc>(docKeys.detail(wsId, doc.id), doc);
      update((tree) => ({ ...tree, docs: tree.docs.map((d) => (d.id === doc.id ? { ...d, ...doc, unread: d.unread } : d)) }));
    },
  });
}

export function useMoveDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, folderId }: { id: string; folderId: string | null }) => api.moveDoc(id, folderId),
    onMutate: ({ id, folderId }) => {
      const previous = qc.getQueryData<DocTree>(docKeys.tree(wsId));
      update((tree) => ({ ...tree, docs: tree.docs.map((d) => (d.id === id ? { ...d, folder_id: folderId } : d)) }));
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(docKeys.tree(wsId), context.previous);
    },
  });
}

export function useDeleteDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (id: string) => api.deleteDoc(id),
    onSuccess: (_data, id) => {
      update((tree) => ({ ...tree, docs: tree.docs.filter((d) => d.id !== id) }));
      qc.removeQueries({ queryKey: docKeys.detail(wsId, id) });
    },
  });
}

export function useMarkDocRead() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (id: string) => api.markDocRead(id),
    onSuccess: (_data, id) => {
      update((tree) => ({ ...tree, docs: tree.docs.map((d) => (d.id === id ? { ...d, unread: false } : d)) }));
      qc.invalidateQueries({ queryKey: docKeys.activity(wsId) });
    },
  });
}

export function useCreateDocFolder() {
  const { update } = useDocTreeCache();
  return useMutation({
    mutationFn: (data: CreateDocFolderRequest) => api.createDocFolder(data),
    onSuccess: (folder) => {
      update((tree) => ({ ...tree, folders: [...tree.folders.filter((f) => f.id !== folder.id), folder] }));
    },
  });
}

export function useRenameDocFolder() {
  const { update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => api.renameDocFolder(id, title),
    onSuccess: (folder) => {
      update((tree) => ({ ...tree, folders: tree.folders.map((f) => (f.id === folder.id ? folder : f)) }));
    },
  });
}

export function useMoveDocFolder() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, parentId }: { id: string; parentId: string | null }) => api.moveDocFolder(id, parentId),
    onMutate: ({ id, parentId }) => {
      const previous = qc.getQueryData<DocTree>(docKeys.tree(wsId));
      update((tree) => ({ ...tree, folders: tree.folders.map((f) => (f.id === id ? { ...f, parent_id: parentId } : f)) }));
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(docKeys.tree(wsId), context.previous);
    },
  });
}

export function useDeleteDocFolder() {
  const { update } = useDocTreeCache();
  return useMutation({
    mutationFn: (id: string) => api.deleteDocFolder(id),
    onSuccess: (_data, id) => {
      update((tree) => ({ ...tree, folders: tree.folders.filter((f) => f.id !== id) }));
    },
  });
}

export function useLinkProjectDoc() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ projectId, docId }: { projectId: string; docId: string }) => api.linkProjectDoc(projectId, docId),
    onSuccess: (_data, { projectId, docId }) => {
      qc.invalidateQueries({ queryKey: docKeys.project(wsId, projectId) });
      qc.invalidateQueries({ queryKey: docKeys.detail(wsId, docId) });
    },
  });
}

export function useUnlinkProjectDoc() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ projectId, docId }: { projectId: string; docId: string }) => api.unlinkProjectDoc(projectId, docId),
    onSuccess: (_data, { projectId, docId }) => {
      qc.invalidateQueries({ queryKey: docKeys.project(wsId, projectId) });
      qc.invalidateQueries({ queryKey: docKeys.detail(wsId, docId) });
    },
  });
}
