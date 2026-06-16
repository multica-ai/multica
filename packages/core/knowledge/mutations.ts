import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { knowledgeKeys } from "./queries";
import type {
  CreateKnowledgeDraftFromCandidateRequest,
  CreateKnowledgeDraftFromIssueRequest,
  CreateKnowledgeFeedbackRequest,
  EvaluateKnowledgeCandidateRequest,
  UpdateKnowledgeRequest,
} from "./types";

export function useUpdateKnowledge() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateKnowledgeRequest) =>
      api.updateKnowledge(id, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useReviewKnowledge() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.reviewKnowledge(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function usePublishKnowledge() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.publishKnowledge(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useArchiveKnowledge() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveKnowledge(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useRestoreKnowledge() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.restoreKnowledge(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useCreateKnowledgeFeedback() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & CreateKnowledgeFeedbackRequest) =>
      api.createKnowledgeFeedback(id, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.detail(wsId, vars.id) });
    },
  });
}

export function useEvaluateKnowledgeCandidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: EvaluateKnowledgeCandidateRequest) =>
      api.evaluateKnowledgeCandidate(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useCreateKnowledgeDraftFromIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateKnowledgeDraftFromIssueRequest) =>
      api.createKnowledgeDraftFromIssue(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}

export function useCreateKnowledgeDraftFromCandidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ candidate_id, regenerate }: CreateKnowledgeDraftFromCandidateRequest) =>
      api.createKnowledgeDraftFromCandidate(candidate_id, { regenerate }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.all(wsId) });
    },
  });
}
