import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { knowledgeKeys } from "./queries";
import type { KnowledgeDocument } from "./types";
import type {
  CreateKnowledgeEvalCaseRequest,
  KnowledgeEvalCase,
  KnowledgeEvalRun,
  KnowledgeKind,
  KnowledgePublishPolicy,
  ReviewKnowledgeDocumentRequest,
  SetKnowledgePolicyRequest,
  UpdateKnowledgeKindRequest,
} from "./taxonomy-types";

// Query keys and hooks of the knowledge taxonomy. Like the rest of the knowledge
// module, no write patches a cache optimistically: each awaits the server and
// refetches. The taxonomy keys live under the workspace's knowledge prefix, so
// invalidating knowledgeKeys.all covers them.

export const knowledgeTaxonomyKeys = {
  kinds: (wsId: string) => [...knowledgeKeys.all(wsId), "kinds"] as const,
  health: (wsId: string) => [...knowledgeKeys.all(wsId), "health"] as const,
  policies: (wsId: string) => [...knowledgeKeys.all(wsId), "policies"] as const,
  evalCases: (wsId: string) => [...knowledgeKeys.all(wsId), "eval-cases"] as const,
  evalRuns: (wsId: string) => [...knowledgeKeys.all(wsId), "eval-runs"] as const,
};

/** The workspace's kinds, with their templates and required headings. */
export function knowledgeKindsOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeTaxonomyKeys.kinds(wsId),
    queryFn: () => api.listKnowledgeKinds(),
  });
}

/** The latest run of each Librarian routine. Owners and admins read the detail. */
export function knowledgeHealthOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeTaxonomyKeys.health(wsId),
    queryFn: () => api.getKnowledgeHealth(),
  });
}

export function knowledgePoliciesOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeTaxonomyKeys.policies(wsId),
    queryFn: () => api.listKnowledgePolicies(),
  });
}

export function knowledgeEvalCasesOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeTaxonomyKeys.evalCases(wsId),
    queryFn: () => api.listKnowledgeEvalCases(),
  });
}

export function knowledgeEvalRunsOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeTaxonomyKeys.evalRuns(wsId),
    queryFn: () => api.listKnowledgeEvalRuns(),
  });
}

/** Owners and admins: edit a kind's name, template, freshness or headings. */
export function useUpdateKnowledgeKind(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeKind, Error, { key: string; patch: UpdateKnowledgeKindRequest }>({
    mutationFn: ({ key, patch }) => api.updateKnowledgeKind(key, patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.kinds(wsId) });
    },
  });
}

/** Owners and admins: set the publish policy of a base or a kind. */
export function useSetKnowledgePolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgePublishPolicy, Error, SetKnowledgePolicyRequest>({
    mutationFn: (req) => api.setKnowledgePolicy(req),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.policies(wsId) });
    },
  });
}

/** Owners and admins: remove a policy, so its scope is manual again. */
export function useDeleteKnowledgePolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { scope: "base" | "kind"; key: string }>({
    mutationFn: ({ scope, key }) => api.deleteKnowledgePolicy(scope, key),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.policies(wsId) });
    },
  });
}

/**
 * Records a critic's verdict on a draft. An agent of another squad publishes it
 * when the policy allows; a member's verdict is recorded and waits for a person.
 */
export function useReviewKnowledgeDocument(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeDocument, Error, { id: string } & ReviewKnowledgeDocumentRequest>({
    mutationFn: ({ id, ...req }) => api.reviewKnowledgeDocument(id, req),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeKeys.documents(wsId) });
    },
  });
}

/** Any member or agent: add a question the search should answer. */
export function useCreateKnowledgeEvalCase(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeEvalCase, Error, CreateKnowledgeEvalCaseRequest>({
    mutationFn: (req) => api.createKnowledgeEvalCase(req),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.evalCases(wsId) });
    },
  });
}

/** Owners and admins: delete a case. */
export function useDeleteKnowledgeEvalCase(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { id: string }>({
    mutationFn: ({ id }) => api.deleteKnowledgeEvalCase(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.evalCases(wsId) });
    },
  });
}

/** Owners and admins: run every case now and record the hit rate. */
export function useRunKnowledgeEval(wsId: string) {
  const qc = useQueryClient();
  return useMutation<KnowledgeEvalRun, Error, void>({
    mutationFn: () => api.runKnowledgeEval(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: knowledgeTaxonomyKeys.evalRuns(wsId) });
    },
  });
}
