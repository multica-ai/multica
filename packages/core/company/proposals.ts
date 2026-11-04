import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import type {
  CreateProposalRequest,
  DecideProposalRequest,
  ListProposalsParams,
  Proposal,
} from "./proposal-schema";

export const proposalKeys = {
  all: (wsId: string) => ["proposals", wsId] as const,
  list: (wsId: string) => [...proposalKeys.all(wsId), "list"] as const,
  /** Full key for a filtered list; the unfiltered list shares the prefix. */
  listFiltered: (wsId: string, params: ListProposalsParams = {}) =>
    [...proposalKeys.list(wsId), params.state ?? "", params.squad_id ?? ""] as const,
  detail: (wsId: string, id: string) => [...proposalKeys.all(wsId), "detail", id] as const,
  revisions: (wsId: string, id: string) => [...proposalKeys.all(wsId), "revisions", id] as const,
};

export function proposalListOptions(wsId: string, params: ListProposalsParams = {}) {
  return queryOptions({
    queryKey: proposalKeys.listFiltered(wsId, params),
    queryFn: () => api.listProposals(params),
  });
}

export function proposalDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: proposalKeys.detail(wsId, id),
    queryFn: () => api.getProposal(id),
    enabled: !!id,
  });
}

/** The revision history, newest first. Fetched only while a view asks for it. */
export function proposalRevisionsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: proposalKeys.revisions(wsId, id),
    queryFn: () => api.listProposalRevisions(id),
    enabled: !!id,
  });
}

/**
 * The refusal code the server gave a proposal request (for example
 * `proposal_revising` or `snooze_out_of_range`), or null when the failure was
 * not a coded refusal.
 */
export function proposalRefusalCode(error: unknown): string | null {
  if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return null;
  const code = (error.body as { code?: unknown }).code;
  return typeof code === "string" && code ? code : null;
}

/**
 * Files a proposal. Awaits the server: the limits are enforced there, and a
 * refusal (409, code `proposal_limit`) is the caller's to show.
 */
export function useCreateProposal(wsId: string) {
  const qc = useQueryClient();
  return useMutation<Proposal, Error, CreateProposalRequest>({
    mutationFn: (data) => api.createProposal(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: proposalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/**
 * Settles a proposal: approve, reject, ask for changes (revise) or put it aside
 * until a date (snooze). Nothing is patched optimistically: an approval moves
 * the backing issue, may create a project and a kickoff issue, and any decision
 * can be refused (409), so every affected cache is refetched once the server
 * has answered.
 */
export function useDecideProposal(wsId: string) {
  const qc = useQueryClient();
  return useMutation<Proposal, Error, { id: string } & DecideProposalRequest>({
    mutationFn: ({ id, ...data }) => api.decideProposal(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: proposalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}
