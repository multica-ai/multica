import { useQuery } from "@tanstack/react-query";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";

/**
 * Fetches pomodoro history entries + aggregate stats.
 * Keyed by workspace ID and pagination params.
 */
export function usePomodoroHistoryQuery(params?: { limit?: number; offset?: number }) {
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");
  return useQuery({
    queryKey: ["pomodoro", "history", workspaceId, params ?? {}],
    queryFn: () => api.getPomodoroHistory(params),
    enabled: !!workspaceId,
    staleTime: 60_000,
  });
}
