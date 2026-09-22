import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { ConfirmKnowledgeBlocksRequest, CreateKnowledgeBaseRequest, CreateKnowledgeProviderRequest, KnowledgeModelSettingsRequest, UpdateKnowledgeBaseRequest, UpdateKnowledgeProviderRequest } from "../types/knowledge";
import { knowledgeKeys } from "./queries";

export function useCreateKnowledgeBase() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateKnowledgeBaseRequest) => api.createKnowledgeBase(data),
    onSuccess: (base) => {
      queryClient.setQueryData(knowledgeKeys.base(workspaceId, base.id), base);
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.bases(workspaceId) });
    },
  });
}

export function useUpdateKnowledgeBase(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: UpdateKnowledgeBaseRequest) => api.updateKnowledgeBase(baseId, data),
    onSuccess: (base) => {
      queryClient.setQueryData(knowledgeKeys.base(workspaceId, base.id), base);
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.bases(workspaceId) });
    },
  });
}

export function useDeleteKnowledgeBase() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ baseId, expectedRevision }: { baseId: string; expectedRevision: number }) => api.deleteKnowledgeBase(baseId, expectedRevision),
    onSuccess: (_result, variables) => {
      queryClient.removeQueries({ queryKey: knowledgeKeys.base(workspaceId, variables.baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.bases(workspaceId) });
    },
  });
}

export function usePutKnowledgeBaseModelSettings(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: KnowledgeModelSettingsRequest) => api.putKnowledgeBaseModelSettings(baseId, data),
    onSuccess: (settings) => {
      queryClient.setQueryData(knowledgeKeys.settings(workspaceId, baseId), settings);
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.base(workspaceId, baseId) });
    },
  });
}

export function usePutKnowledgeModelSettings() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: KnowledgeModelSettingsRequest) => api.putKnowledgeModelSettings(data),
    onSuccess: (settings) => {
      queryClient.setQueryData(knowledgeKeys.settings(workspaceId), settings);
    },
  });
}

export function useCreateKnowledgeProvider() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateKnowledgeProviderRequest) => api.createKnowledgeProvider(data),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.providers(workspaceId) });
    },
  });
}

export function useUpdateKnowledgeProvider() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ providerId, data }: { providerId: string; data: UpdateKnowledgeProviderRequest }) => api.updateKnowledgeProvider(providerId, data),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.providers(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.settings(workspaceId) });
    },
  });
}

export function useDeleteKnowledgeProvider() {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (providerId: string) => api.deleteKnowledgeProvider(providerId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.providers(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.settings(workspaceId) });
    },
  });
}

export function useCreateKnowledgeURL(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: { url: string; title?: string; tags?: string[]; metadata?: Record<string, unknown> }) => api.createKnowledgeURL(baseId, data),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.base(workspaceId, baseId) });
    },
  });
}

export function useUploadKnowledgeFile(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ file, title, tags }: { file: File; title?: string; tags?: string[] }) => api.uploadKnowledgeFile(baseId, file, title, tags),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.base(workspaceId, baseId) });
    },
  });
}

export function useCreateKnowledgeIndex(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.createKnowledgeIndex(baseId),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.base(workspaceId, baseId) });
    },
  });
}

export function useReprocessKnowledgeDocument(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ documentId, stage }: { documentId: string; stage?: string }) => api.reprocessKnowledgeDocument(baseId, documentId, stage),
    onSettled: (_data, _error, variables) => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.document(workspaceId, baseId, variables.documentId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
    },
  });
}

export function useReplaceKnowledgeFile(baseId: string, documentId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ file, expectedRevision }: { file: File; expectedRevision: number }) => api.replaceKnowledgeFile(baseId, documentId, file, expectedRevision),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.document(workspaceId, baseId, documentId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
    },
  });
}

export function useReplaceKnowledgeURL(baseId: string, documentId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: { url: string; expected_revision: number; metadata?: Record<string, unknown> }) => api.replaceKnowledgeURL(baseId, documentId, data),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.document(workspaceId, baseId, documentId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
    },
  });
}

export function useConfirmKnowledgeBlocks(baseId: string, documentId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: ConfirmKnowledgeBlocksRequest) => api.confirmKnowledgeBlocks(baseId, documentId, data),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.document(workspaceId, baseId, documentId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
    },
  });
}

export function useIssueKnowledgePreviewCapability(baseId: string, documentId: string, versionId: string) {
  return useMutation({
    mutationFn: () => api.issueKnowledgePreviewCapability(baseId, documentId, versionId),
  });
}

export function useCancelKnowledgeJob(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jobId: string) => api.cancelKnowledgeJob(baseId, jobId),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.jobs(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
    },
  });
}

export function useApplyKnowledgeGraphEdit(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: { operation: string; targetId: string; payload: Record<string, unknown>; expectedRevision: number }) => api.applyKnowledgeGraphEdit(baseId, data),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.all(workspaceId) });
    },
  });
}

export function useRevertKnowledgeGraphEdit(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (editId: string) => api.revertKnowledgeGraphEdit(baseId, editId),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.all(workspaceId) });
    },
  });
}

export function useAnswerKnowledge(baseId: string) {
  return useMutation({
    mutationFn: (data: { question: string; limit?: number; mode?: string }) => api.answerKnowledge(baseId, data),
  });
}

export function useSearchKnowledge(baseId: string) {
  return useMutation({
    mutationFn: (data: { query: string; limit?: number; mode?: string }) => api.searchKnowledge(baseId, data),
  });
}

export function useDeleteKnowledgeDocument(baseId: string) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ documentId, expectedRevision }: { documentId: string; expectedRevision: number }) => api.deleteKnowledgeDocument(baseId, documentId, expectedRevision),
    onSuccess: (_result, variables) => {
      queryClient.removeQueries({ queryKey: knowledgeKeys.document(workspaceId, baseId, variables.documentId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(workspaceId, baseId) });
      void queryClient.invalidateQueries({ queryKey: knowledgeKeys.base(workspaceId, baseId) });
    },
  });
}
