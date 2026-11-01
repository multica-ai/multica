import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { knowledgeKeys } from "./queries";
import type { CreateKnowledgeDocumentRequest, KnowledgeDocument } from "./types";

// None of these patch a cache optimistically: whether a write lands depends on
// the knowledge service, which can be offline, and publishing changes what
// search returns. Each awaits the server and refetches once it has answered.

/**
 * Writes a document. An agent or a member without `publish` creates a draft;
 * a member's `publish: true` indexes it at once. A failure carries the
 * knowledge state; read it with `knowledgeErrorState`.
 */
export function useCreateKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeDocument, Error, CreateKnowledgeDocumentRequest>({
    mutationFn: (data) => api.createKnowledgeDocument(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.documents(wsId) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.bases(wsId) });
    },
  });
}

/** Publishes a draft, which makes it searchable. Owners and admins only. */
export function usePublishKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeDocument, Error, { id: string }>({
    mutationFn: ({ id }) => api.publishKnowledgeDocument(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

/** Deletes a draft. Owners and admins only. */
export function useDiscardKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { id: string }>({
    mutationFn: ({ id }) => api.discardKnowledgeDocument(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.documents(wsId) });
    },
  });
}

/** Deletes a document in any state. Owners and admins only. */
export function useDeleteKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { id: string }>({
    mutationFn: ({ id }) => api.deleteKnowledgeDocument(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}
