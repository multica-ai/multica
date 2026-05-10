import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { DailyPlan } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

/** Fetches tomorrow's daily plan draft. Returns null/undefined if none exists yet. */
export function useTomorrowPlanQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<DailyPlan | null>({
    queryKey: queryKeys.dailyPlan.tomorrow(workspaceId),
    queryFn: () => api.getTomorrowPlan(),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}

/** Fetches the list of recent daily plans. */
export function useDailyPlansQuery(limit = 30) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<DailyPlan[]>({
    queryKey: queryKeys.dailyPlan.list(workspaceId),
    queryFn: () => api.listDailyPlans(limit),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}

/** Triggers AI generation of tomorrow's plan draft. Invalidates tomorrow + list caches. */
export function useGeneratePlanMutation() {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation<DailyPlan, Error, string | undefined>({
    mutationFn: (planDate) => api.generateDailyPlan(planDate),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.dailyPlan.tomorrow(workspaceId) });
      qc.invalidateQueries({ queryKey: queryKeys.dailyPlan.list(workspaceId) });
    },
  });
}

/** Confirms (signs off) a daily plan by ID. Invalidates tomorrow + list caches. */
export function useConfirmPlanMutation() {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation<DailyPlan, Error, string>({
    mutationFn: (planId) => api.confirmDailyPlan(planId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.dailyPlan.tomorrow(workspaceId) });
      qc.invalidateQueries({ queryKey: queryKeys.dailyPlan.list(workspaceId) });
    },
  });
}
