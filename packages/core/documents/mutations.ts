import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { documentKeys } from "./queries";
import type { UpsertDocumentRequest, RestoreDocumentRequest } from "../types";

export function useUpsertDocument() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ path, ...data }: { path: string } & UpsertDocumentRequest) =>
      api.upsertDocumentByPath(path, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: documentKeys.all(wsId) });
    },
  });
}

export function useUpdateDocumentContent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ path, ...data }: { path: string } & UpsertDocumentRequest) =>
      api.upsertDocumentByPath(path, data),
    onSuccess: (doc) => {
      qc.setQueryData(documentKeys.detail(wsId, doc.id), doc);
      qc.invalidateQueries({ queryKey: documentKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: documentKeys.revisions(wsId, doc.id) });
    },
  });
}

export function usePinDocument() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, pinned }: { id: string; pinned: boolean }) =>
      pinned ? api.pinDocument(id) : api.unpinDocument(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: documentKeys.all(wsId) });
    },
  });
}

export function useArchiveDocument() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason?: string }) =>
      api.archiveDocument(id, reason),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: documentKeys.all(wsId) });
    },
  });
}

export function useRestoreDocumentRevision() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & RestoreDocumentRequest) =>
      api.restoreDocument(id, data),
    onSuccess: (doc) => {
      qc.setQueryData(documentKeys.detail(wsId, doc.id), doc);
      qc.invalidateQueries({ queryKey: documentKeys.revisions(wsId, doc.id) });
    },
  });
}

export function useRenameDocument() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, newPath }: { id: string; newPath: string }) =>
      api.renameDocument(id, { new_path: newPath }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: documentKeys.all(wsId) });
    },
  });
}

export function useUpdateDocumentTags() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, add, remove }: { id: string; add?: string[]; remove?: string[] }) =>
      api.updateDocumentTags(id, { add, remove }),
    onSuccess: (doc) => {
      qc.setQueryData(documentKeys.detail(wsId, doc.id), doc);
      qc.invalidateQueries({ queryKey: documentKeys.list(wsId) });
    },
  });
}
