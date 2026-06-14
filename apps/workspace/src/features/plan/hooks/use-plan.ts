import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type {
  CreatePlanItemRequest,
  Plan,
  PlanCandidatesResponse,
  UpdatePlanItemRequest,
  UpsertPlanRequest,
} from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

export function usePlanQuery(date: string) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<Plan>({
    queryKey: queryKeys.plan.byDate(workspaceId, date),
    queryFn: () => api.getPlan(date),
    enabled: !!workspaceId,
    staleTime: 30_000,
  });
}

export function usePlanCandidatesQuery(date: string, issueTypeId?: string) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery<PlanCandidatesResponse>({
    queryKey: queryKeys.plan.candidates(workspaceId, date, issueTypeId),
    queryFn: () => api.listPlanCandidates(date, issueTypeId),
    enabled: !!workspaceId,
    staleTime: 30_000,
  });
}

export function useUpsertPlanMutation(date: string) {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation<Plan, Error, UpsertPlanRequest>({
    mutationFn: (body) => api.upsertPlan(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.plan.byDate(workspaceId, date) });
      qc.invalidateQueries({ queryKey: queryKeys.plan.candidates(workspaceId, date) });
    },
  });
}

export function useCreatePlanItemMutation(date: string, planId: string) {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation({
    mutationFn: (body: CreatePlanItemRequest) => api.createPlanItem(planId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.plan.byDate(workspaceId, date) });
      qc.invalidateQueries({ queryKey: queryKeys.plan.candidates(workspaceId, date) });
    },
  });
}

export function useUpdatePlanItemMutation(date: string) {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation({
    mutationFn: ({ itemId, body }: { itemId: string; body: UpdatePlanItemRequest }) =>
      api.updatePlanItem(itemId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.plan.byDate(workspaceId, date) });
      qc.invalidateQueries({ queryKey: queryKeys.plan.candidates(workspaceId, date) });
    },
  });
}

export function useStartPlanItemFocusMutation(date: string) {
  const qc = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useMutation({
    mutationFn: ({ itemId, issueId, title }: { itemId: string; issueId?: string | null; title: string }) =>
      api.startPlanItemFocus(itemId, {
        mode: "flowtime",
        issue_id: issueId ?? null,
        commitment_text: title,
        timer_conflict_action: "cancel",
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.plan.byDate(workspaceId, date) });
      qc.invalidateQueries({ queryKey: queryKeys.focus.current(workspaceId) });
    },
  });
}
