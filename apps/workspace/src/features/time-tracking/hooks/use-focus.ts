import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import type {
  CompleteFocusRequest,
  FocusReasonRequest,
  StartFocusRequest,
  UpdateFocusRequest,
} from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

/** Fetches the current Focus session for the selected workspace. */
export function useFocusQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: queryKeys.focus.current(workspaceId),
    queryFn: () => api.getFocusSession().then((response) => response.session),
    refetchInterval: 10_000,
    refetchOnWindowFocus: true,
    enabled: !!workspaceId,
  });
}

/** Fetches persisted Focus events for the current session. */
export function useFocusEventsQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: queryKeys.focus.events(workspaceId),
    queryFn: () => api.listFocusEvents().then((response) => response.events),
    enabled: !!workspaceId,
    staleTime: 30_000,
  });
}

function useFocusInvalidation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return () => {
    if (!workspaceId) return;
    queryClient.invalidateQueries({ queryKey: queryKeys.focus.current(workspaceId) });
    queryClient.invalidateQueries({ queryKey: queryKeys.focus.events(workspaceId) });
    queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
  };
}

/** Starts a Focus session in Pomodoro, Flowtime, or quick-start mode. */
export function useStartFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body: StartFocusRequest) => api.startFocus(body),
    onSuccess: invalidate,
  });
}

/** Updates the current Focus context draft. */
export function useUpdateFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body: UpdateFocusRequest) => api.updateFocus(body),
    onSuccess: invalidate,
  });
}

/** Pauses the current Focus session with an optional reason. */
export function usePauseFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body?: FocusReasonRequest) => api.pauseFocus(body),
    onSuccess: invalidate,
  });
}

/** Resumes a paused Focus session. */
export function useResumeFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: () => api.resumeFocus(),
    onSuccess: invalidate,
  });
}

/** Completes the current Focus session and writes its time entry. */
export function useCompleteFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body?: CompleteFocusRequest) => api.completeFocus(body),
    onSuccess: invalidate,
  });
}

/** Abandons the current Focus session without creating a time entry. */
export function useAbandonFocusMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body?: FocusReasonRequest) => api.abandonFocus(body),
    onSuccess: invalidate,
  });
}

/** Starts the suggested break for the completed Focus session. */
export function useStartFocusBreakMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: () => api.startFocusBreak(),
    onSuccess: invalidate,
  });
}

/** Skips the suggested break with an optional reason. */
export function useSkipFocusBreakMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: (body?: FocusReasonRequest) => api.skipFocusBreak(body),
    onSuccess: invalidate,
  });
}

/** Completes the current break and records a break event. */
export function useCompleteFocusBreakMutation() {
  const invalidate = useFocusInvalidation();
  return useMutation({
    mutationFn: () => api.completeFocusBreak(),
    onSuccess: invalidate,
  });
}
