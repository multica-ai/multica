import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { PomodoroSession } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

// ── Query ──────────────────────────────────────────────────────────────────────

/**
 * Fetches the current pomodoro session every 10 seconds.
 * Returns an idle-default object when no active session exists.
 * Skips the request when there is no workspace loaded yet.
 */
export function usePomodoroQuery() {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: queryKeys.pomodoro.current(workspaceId),
    queryFn: () => api.getPomodoroSession(),
    refetchInterval: 10_000,
    refetchOnWindowFocus: true,
    enabled: !!workspaceId,
  });
}

// ── Mutations ──────────────────────────────────────────────────────────────────

/** Start or resume the pomodoro timer. Optimistically sets status → "running". */
export function useStartPomodoroMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: () => api.startPomodoro(),
    onMutate: async () => {
      const key = queryKeys.pomodoro.current(workspaceId);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<PomodoroSession>(key);
      if (previous) {
        queryClient.setQueryData<PomodoroSession>(key, {
          ...previous,
          status: "running",
          started_at: new Date().toISOString(),
        });
      }
      return { previous };
    },
    onError: (_err, _vars, context) => {
      const key = queryKeys.pomodoro.current(workspaceId);
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },
    onSuccess: (session) => {
      queryClient.setQueryData(queryKeys.pomodoro.current(workspaceId), session);
    },
  });
}

/** Pause the running pomodoro timer. Optimistically sets status → "paused". */
export function usePausePomodoroMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: () => api.pausePomodoro(),
    onMutate: async () => {
      const key = queryKeys.pomodoro.current(workspaceId);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<PomodoroSession>(key);
      if (previous) {
        // Calculate accumulated elapsed time before pause.
        const runningFor = previous.started_at
          ? (Date.now() - new Date(previous.started_at).getTime()) / 1000
          : 0;
        queryClient.setQueryData<PomodoroSession>(key, {
          ...previous,
          status: "paused",
          elapsed_seconds: previous.elapsed_seconds + runningFor,
          started_at: null,
        });
      }
      return { previous };
    },
    onError: (_err, _vars, context) => {
      const key = queryKeys.pomodoro.current(workspaceId);
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },
    onSuccess: (session) => {
      queryClient.setQueryData(queryKeys.pomodoro.current(workspaceId), session);
    },
  });
}

/**
 * Complete the current phase.
 * Optimistically flips the phase and resets elapsed.
 * Work-phase completion creates a pomodoro time_entry on the backend.
 */
export function useCompletePomodoroMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: () => api.completePomodoro(),
    onMutate: async () => {
      const key = queryKeys.pomodoro.current(workspaceId);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<PomodoroSession>(key);
      if (previous) {
        const nextPhase = previous.phase === "work" ? "break" : "work";
        // Default durations: 25 min work, 5 min break.
        const nextDuration = nextPhase === "work" ? 25 * 60 : 5 * 60;
        queryClient.setQueryData<PomodoroSession>(key, {
          ...previous,
          phase: nextPhase,
          phase_duration_seconds: nextDuration,
          status: "idle",
          elapsed_seconds: 0,
          started_at: null,
        });
      }
      return { previous };
    },
    onError: (_err, _vars, context) => {
      const key = queryKeys.pomodoro.current(workspaceId);
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },
    onSuccess: (session) => {
      queryClient.setQueryData(queryKeys.pomodoro.current(workspaceId), session);
      // Invalidate time entries so the newly created pomodoro entry appears in the list.
      queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(workspaceId) });
    },
  });
}

/** Reset the pomodoro session to idle. Optimistically clears all state. */
export function useResetPomodoroMutation() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  return useMutation({
    mutationFn: () => api.resetPomodoro(),
    onMutate: async () => {
      const key = queryKeys.pomodoro.current(workspaceId);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<PomodoroSession>(key);
      queryClient.setQueryData<PomodoroSession>(key, {
        phase: "work",
        phase_duration_seconds: 25 * 60,
        status: "idle",
        elapsed_seconds: 0,
        started_at: null,
      });
      return { previous };
    },
    onError: (_err, _vars, context) => {
      const key = queryKeys.pomodoro.current(workspaceId);
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },
    onSuccess: (session) => {
      queryClient.setQueryData(queryKeys.pomodoro.current(workspaceId), session);
    },
  });
}
