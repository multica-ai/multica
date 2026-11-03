import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { proposalKeys } from "../office/proposals";
import type { DecideProposalRequest, Proposal } from "../office/proposal-schema";
import { initiativeKeys } from "./queries";
import type { CreateInitiativeRequest, ForceInitiativeRequest, Initiative } from "./types";

/*
 * Every transition here waits for the server: a stage move can be refused
 * (preconditions, permissions) and changes the backing issue, so nothing is
 * patched optimistically. Each one refetches the initiatives it may touch.
 */

/** Admin override: moves an initiative to any stage with a reason. Refused for non-admins. */
export function useForceInitiative(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { id: string } & ForceInitiativeRequest>({
    mutationFn: ({ id, ...data }) => api.forceInitiative(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: initiativeKeys.all(wsId) });
    },
  });
}

/** Waives a required artifact of the current stage. The note is recorded with the waiver. */
export function useWaiveInitiativeArtifact(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, { id: string; kind: string; note: string }>({
    mutationFn: ({ id, kind, note }) => api.waiveInitiativeArtifact(id, kind, note),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: initiativeKeys.all(wsId) });
    },
  });
}

/**
 * Approves or rejects a stage gate. A gate is a `stage_gate` proposal, so the
 * decision goes through the proposal service; an approval advances the
 * initiative, so the initiative queries are refetched as well as the proposals.
 */
export function useDecideInitiativeGate(wsId: string) {
  const qc = useQueryClient();
  return useMutation<
    Proposal,
    Error,
    { id: string } & Extract<DecideProposalRequest, { decision: "approve" | "reject" }>
  >({
    mutationFn: ({ id, ...data }) => api.decideProposal(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: proposalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: initiativeKeys.all(wsId) });
    },
  });
}

/**
 * Starts an initiative at its pipeline's first stage. Refused for non-admins.
 * The backing issue is created with it, so the issues are refetched too.
 */
export function useCreateInitiative(wsId: string) {
  const qc = useQueryClient();
  return useMutation<Initiative, Error, CreateInitiativeRequest>({
    mutationFn: (data) => api.createInitiative(data),
    onSettled: () => refreshAfterTransition(qc, wsId),
  });
}

/** The stage transitions share one refresh: a move changes the backing issue as well as the board. */
function useInitiativeTransition<Vars extends { id: string }>(
  wsId: string,
  run: (vars: Vars) => Promise<Initiative>,
) {
  const qc = useQueryClient();
  return useMutation<Initiative, Error, Vars>({
    mutationFn: run,
    onSettled: () => refreshAfterTransition(qc, wsId),
  });
}

function refreshAfterTransition(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: initiativeKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
}

/** Moves the initiative past its current stage when the exit allows it. */
export function useAdvanceInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string }>(wsId, ({ id }) => api.advanceInitiative(id));
}

/** Records the owning squad's attestation of an attest-mode stage. */
export function useAttestInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string }>(wsId, ({ id }) => api.attestInitiative(id));
}

/** Returns the initiative to the previous stage. The note goes to the leader and is required. */
export function useSendBackInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string; note: string }>(wsId, ({ id, note }) =>
    api.sendBackInitiative(id, note),
  );
}

/** Pauses the initiative. The note is optional. */
export function useShelveInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string; note: string }>(wsId, ({ id, note }) =>
    api.shelveInitiative(id, note),
  );
}

/** Brings a shelved initiative back. */
export function useResumeInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string }>(wsId, ({ id }) => api.resumeInitiative(id));
}

/** Ends the initiative. The reason is required, and the backing issue is closed with it. */
export function useCloseInitiative(wsId: string) {
  return useInitiativeTransition<{ id: string; reason: string }>(wsId, ({ id, reason }) =>
    api.closeInitiative(id, reason),
  );
}
