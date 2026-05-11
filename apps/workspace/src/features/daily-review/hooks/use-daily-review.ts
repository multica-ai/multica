import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { DailyReview } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

/** Fetches today's daily review draft. Returns null/undefined if none exists yet. */
export function useTodayReviewQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<DailyReview | null>({
    queryKey: queryKeys.dailyReview.today(workspaceId),
    queryFn: () => api.getTodayReview(),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}

/** Fetches the list of recent daily reviews. */
export function useDailyReviewsQuery(limit = 30) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<DailyReview[]>({
    queryKey: queryKeys.dailyReview.list(workspaceId),
    queryFn: () => api.listDailyReviews(limit),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}

/** Triggers AI generation of today's review draft. Invalidates today + list caches. */
export function useGenerateReviewMutation() {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation<DailyReview, Error>({
    mutationFn: () => api.generateDailyReview(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.dailyReview.today(workspaceId) });
      qc.invalidateQueries({ queryKey: queryKeys.dailyReview.list(workspaceId) });
    },
  });
}

/** Confirms (signs off) a daily review by ID. Invalidates today + list caches. */
export function useConfirmReviewMutation() {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation<DailyReview, Error, string>({
    mutationFn: (reviewId) => api.confirmDailyReview(reviewId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.dailyReview.today(workspaceId) });
      qc.invalidateQueries({ queryKey: queryKeys.dailyReview.list(workspaceId) });
    },
  });
}
