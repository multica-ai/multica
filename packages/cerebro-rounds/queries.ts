import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { inboxKeys } from "@multica/core/inbox/queries";
import { addIssueToRound, createRound, deleteRound, getRoundStatus, listRounds, pauseRound, removeIssueFromRound, startRound, updateRound, type RoundInput } from "./api";

export const roundKeys = { all: (wsId: string) => ["cerebro-rounds", wsId] as const };

export function useRoundStatuses(wsId: string, enabled = true) {
  return useQuery({ queryKey: roundKeys.all(wsId), enabled, queryFn: async () => {
    const rounds = await listRounds();
    return (await Promise.all(rounds.map((r) => getRoundStatus(r.id)))).filter((v) => v !== null);
  } });
}
function useInvalidate(wsId: string) { const qc = useQueryClient(); return () => qc.invalidateQueries({ queryKey: roundKeys.all(wsId) }); }
export function useStartRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: startRound, onSettled: invalidate }); }
export function usePauseRound(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: pauseRound,
    onSettled: () => Promise.all([
      qc.invalidateQueries({ queryKey: roundKeys.all(wsId) }),
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) }),
    ]),
  });
}
export function useAddIssueToRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: ({ roundId, issueId }: {roundId:string; issueId:string}) => addIssueToRound(roundId, issueId), onSettled: invalidate }); }
export function useCreateRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: createRound, onSettled: invalidate }); }
export function useUpdateRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: ({ id, input }: { id: string; input: RoundInput }) => updateRound(id, input), onSettled: invalidate }); }
export function useDeleteRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: deleteRound, onSettled: invalidate }); }
export function useRemoveIssueFromRound(wsId: string) { const invalidate = useInvalidate(wsId); return useMutation({ mutationFn: ({ roundId, issueId }: { roundId: string; issueId: string }) => removeIssueFromRound(roundId, issueId), onSettled: invalidate }); }
