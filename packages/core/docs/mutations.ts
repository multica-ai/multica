import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateDocCommentRequest,
  CreateDocFolderRequest,
  CreateDocRequest,
  Doc,
  DocTree,
  UpdateDocRequest,
} from "../types";
import { docKeys } from "./queries";

function useDocTreeCache() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const update = (fn: (tree: DocTree) => DocTree) =>
    qc.setQueryData<DocTree>(docKeys.tree(wsId), (old) =>
      old ? fn(old) : old,
    );
  return { qc, wsId, update };
}

export function useCreateDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (data: CreateDocRequest) => api.createDoc(data),
    onSuccess: (doc) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      qc.setQueryData(docKeys.detail(wsId, doc.id), doc);
      update((tree) => ({
        ...tree,
        docs: [...tree.docs.filter((d) => d.id !== doc.id), doc],
      }));
    },
  });
}

export function useUpdateDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateDocRequest) =>
      api.updateDoc(id, data),
    onSuccess: (doc) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      qc.setQueryData<Doc>(docKeys.detail(wsId, doc.id), doc);
      update((tree) => ({
        ...tree,
        docs: tree.docs.map((d) => (d.id === doc.id ? { ...d, ...doc } : d)),
      }));
    },
  });
}

export function useMoveDoc() {
  const { qc, wsId } = useDocTreeCache();
  return useMutation({
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
    },
    mutationFn: ({ id, folderId }: { id: string; folderId: string | null }) =>
      api.moveDoc(id, folderId),
  });
}

export function useDeleteDoc() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (id: string) => api.deleteDoc(id),
    onSuccess: (_data, id) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      update((tree) => ({
        ...tree,
        docs: tree.docs.filter((d) => d.id !== id),
      }));
      qc.removeQueries({ queryKey: docKeys.detail(wsId, id) });
    },
  });
}

export function useCreateDocFolder() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (data: CreateDocFolderRequest) => api.createDocFolder(data),
    onSuccess: (folder) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      update((tree) => ({
        ...tree,
        folders: [...tree.folders.filter((f) => f.id !== folder.id), folder],
      }));
    },
  });
}

export function useRenameDocFolder() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.renameDocFolder(id, title),
    onSuccess: (folder) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      update((tree) => ({
        ...tree,
        folders: tree.folders.map((f) => (f.id === folder.id ? folder : f)),
      }));
    },
  });
}

export function useMoveDocFolder() {
  const { qc, wsId } = useDocTreeCache();
  return useMutation({
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
    },
    mutationFn: ({ id, parentId }: { id: string; parentId: string | null }) =>
      api.moveDocFolder(id, parentId),
  });
}

export function useDeleteDocFolder() {
  const { qc, wsId, update } = useDocTreeCache();
  return useMutation({
    mutationFn: (id: string) => api.deleteDocFolder(id),
    onSuccess: (_data, id) => {
      qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      update((tree) => ({
        ...tree,
        folders: tree.folders.filter((f) => f.id !== id),
      }));
    },
  });
}

/** Comment writes refetch the doc's discussions; the server is the source of truth. */
function useDocCommentsInvalidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return (docId: string) => qc.invalidateQueries({ queryKey: docKeys.comments(wsId, docId) });
}

export function useCreateDocComment(docId: string) {
  const invalidate = useDocCommentsInvalidate();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateDocCommentRequest) => api.createDocComment(docId, data),
    onSettled: () => {
      invalidate(docId);
      // A comment can start, or fold into, an agent's run.
      qc.invalidateQueries({ queryKey: docKeys.tasks(wsId, docId) });
    },
  });
}

/** Stops an agent's run on one of the doc's discussions. */
export function useCancelDocTaskRun(docId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (taskId: string) => api.cancelTaskById(taskId),
    onSettled: () => qc.invalidateQueries({ queryKey: docKeys.tasks(wsId, docId) }),
  });
}

export function useSetDocCommentResolved(docId: string) {
  const invalidate = useDocCommentsInvalidate();
  return useMutation({
    mutationFn: ({ commentId, resolved }: { commentId: string; resolved: boolean }) =>
      api.setDocCommentResolved(docId, commentId, resolved),
    onSettled: () => invalidate(docId),
  });
}

export function useDeleteDocComment(docId: string) {
  const invalidate = useDocCommentsInvalidate();
  return useMutation({
    mutationFn: (commentId: string) => api.deleteDocComment(docId, commentId),
    onSettled: () => invalidate(docId),
  });
}
