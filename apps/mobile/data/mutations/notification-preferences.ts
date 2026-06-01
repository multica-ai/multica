/**
 * Mobile notification-preferences mutation. Mirrors the optimistic pattern of
 * packages/core/notification-preferences/mutations.ts — written here per the
 * "Mobile-owned updaters" rule (don't import web mutations; key shape is
 * independent and may drift).
 *
 * Optimistic policy: patch cache → fire PUT → rollback on error → invalidate
 * on settle (mirrors mobile inbox mutations + CLAUDE.md "Mutations are
 * optimistic by default"). Toggle latency on cellular is real — the Switch
 * snapping back if the request hangs would look broken.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  NotificationPreferenceResponse,
  NotificationPreferences,
} from "@multica/core/types";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceKeys } from "@/data/queries/notification-preferences";

export function useUpdateNotificationPreferences() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (preferences: NotificationPreferences) =>
      api.updateNotificationPreferences(preferences),
    onMutate: async (preferences) => {
      const key = notificationPreferenceKeys.all(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<NotificationPreferenceResponse>(key);
      qc.setQueryData<NotificationPreferenceResponse>(key, (old) =>
        old
          ? { ...old, preferences }
          : { workspace_id: wsId ?? "", preferences },
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
    },
    onSettled: (_d, _e, _v, ctx) => {
      qc.invalidateQueries({ queryKey: ctx?.key ?? notificationPreferenceKeys.all(wsId) });
    },
  });
}
