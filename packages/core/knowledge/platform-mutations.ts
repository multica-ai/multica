import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { knowledgeKeys } from "./queries";
import { knowledgePlatformKeys } from "./platform-queries";
import type {
  KnowledgeAccess,
  KnowledgeBaseSettings,
  KnowledgeBaseSettingsPatch,
  KnowledgeModelChecks,
  KnowledgeOwnerLogin,
  KnowledgeSubjectType,
  UploadKnowledgeDocumentParams,
} from "./platform-types";
import type { KnowledgeDocument, KnowledgeVisibility } from "./types";

// As in mutations.ts, nothing is patched optimistically: each call awaits the
// server, and the affected queries are refetched once it has answered.

/**
 * Uploads a file and publishes it at once. A person is the curator, so there
 * is no draft step. Refused above the server's size limit (413).
 */
export function useUploadKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeDocument, Error, UploadKnowledgeDocumentParams>({
    mutationFn: (params) => api.uploadKnowledgeDocument(params),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.documents(wsId) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.bases(wsId) });
    },
  });
}

/** Makes a base restricted or workspace-visible. Owners and admins only. */
export function useSetKnowledgeVisibility(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeAccess, Error, { key: string; visibility: KnowledgeVisibility }>({
    mutationFn: ({ key, visibility }) => api.setKnowledgeVisibility(key, visibility),
    onSettled: (_data, _err, { key }) => {
      qc.invalidateQueries({ queryKey: knowledgePlatformKeys.access(wsId, key) });
      qc.invalidateQueries({ queryKey: knowledgeKeys.bases(wsId) });
    },
  });
}

/** Gives a squad, floor or agent read or write access to a restricted base. */
export function usePutKnowledgeGrant(wsId: string) {
  const qc = useQueryClient();
  return useMutation<
    KnowledgeAccess,
    Error,
    { key: string; subjectType: KnowledgeSubjectType; subjectId: string; access: "read" | "write" }
  >({
    mutationFn: ({ key, subjectType, subjectId, access }) =>
      api.putKnowledgeGrant(key, subjectType, subjectId, access),
    onSettled: (_data, _err, { key }) => {
      qc.invalidateQueries({ queryKey: knowledgePlatformKeys.access(wsId, key) });
    },
  });
}

/** Withdraws a grant. */
export function useRemoveKnowledgeGrant(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeAccess, Error, { key: string; subjectType: KnowledgeSubjectType; subjectId: string }>({
    mutationFn: ({ key, subjectType, subjectId }) => api.deleteKnowledgeGrant(key, subjectType, subjectId),
    onSettled: (_data, _err, { key }) => {
      qc.invalidateQueries({ queryKey: knowledgePlatformKeys.access(wsId, key) });
    },
  });
}

/** Turns Wiki or document summaries on or off for one base. */
export function useUpdateKnowledgeBaseSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeBaseSettings, Error, { key: string } & KnowledgeBaseSettingsPatch>({
    mutationFn: ({ key, ...patch }) => api.updateKnowledgeBaseSettings(key, patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.bases(wsId) });
    },
  });
}

/** Runs each configured model once. Owners and admins only. */
export function useTestKnowledgeModels() {
  return useMutation<KnowledgeModelChecks, Error, void>({
    mutationFn: () => api.testKnowledgeModels(),
  });
}

/**
 * Shows the caller's WeKnora password once. The server clears it with the
 * same call, so the result must be kept by the caller for display only.
 */
export function useRevealKnowledgeOwnerPassword() {
  return useMutation<KnowledgeOwnerLogin, Error, void>({
    mutationFn: () => api.revealKnowledgeOwnerPassword(),
  });
}
