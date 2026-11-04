import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { BriefFeedback, BriefFeedbackSummary, SubmitBriefFeedbackRequest } from "./brief-feedback-schema";

export const briefFeedbackKeys = {
  all: (wsId: string) => ["company", "brief-feedback", wsId] as const,
  mine: (wsId: string, briefId: string) => [...briefFeedbackKeys.all(wsId), "mine", briefId] as const,
  summary: (wsId: string) => [...briefFeedbackKeys.all(wsId), "summary"] as const,
};

/** The caller's own rating of a brief, or null when they have not rated it. */
export function myBriefFeedbackOptions(wsId: string, briefId: string) {
  return queryOptions({
    queryKey: briefFeedbackKeys.mine(wsId, briefId),
    queryFn: () => api.getMyBriefFeedback(briefId),
    enabled: !!wsId && !!briefId,
  });
}

/**
 * The rolling usefulness figure. Owners and admins only: the server answers 403
 * to anyone else, so the query does not retry and the caller hides the figure on error.
 */
export function briefFeedbackSummaryOptions(wsId: string) {
  return queryOptions({
    queryKey: briefFeedbackKeys.summary(wsId),
    queryFn: (): Promise<BriefFeedbackSummary | null> => api.getBriefFeedbackSummary(),
    enabled: !!wsId,
    staleTime: 60_000,
    retry: false,
  });
}

/** Saves the caller's rating and refreshes the summary; awaits the server, nothing is patched early. */
export function useSubmitBriefFeedback(wsId: string, briefId: string) {
  const qc = useQueryClient();
  return useMutation<BriefFeedback | null, Error, SubmitBriefFeedbackRequest>({
    mutationFn: (data) => api.submitBriefFeedback(briefId, data),
    onSuccess: (saved) => {
      qc.setQueryData(briefFeedbackKeys.mine(wsId, briefId), saved);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: briefFeedbackKeys.summary(wsId) });
    },
  });
}
