import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { notificationPreferenceKeys } from "./queries";
import type { NotificationPreferences, NotificationPreferenceResponse } from "../types";

export function useUpdateNotificationPreferences() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (preferences: NotificationPreferences) =>
      api.updateNotificationPreferences(preferences),
    onMutate: async (preferences) => {
      await qc.cancelQueries({ queryKey: notificationPreferenceKeys.all(wsId) });
      const prev = qc.getQueryData<NotificationPreferenceResponse>(
        notificationPreferenceKeys.all(wsId),
      );
      qc.setQueryData<NotificationPreferenceResponse>(
        notificationPreferenceKeys.all(wsId),
        (old) => old ? { ...old, preferences } : { workspace_id: wsId, preferences },
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(notificationPreferenceKeys.all(wsId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notificationPreferenceKeys.all(wsId) });
    },
  });
}
