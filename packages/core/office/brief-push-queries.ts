import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  BriefPushBinding,
  BriefPushPreference,
  UpdateBriefPushRequest,
} from "./brief-push-schema";

export const briefPushKeys = {
  /** Account-level: the preference belongs to the caller, scoped by workspace. */
  mine: (wsId: string) => ["office", "brief-push", wsId] as const,
  /** The caller's own chats that can take briefs in this workspace. */
  bindings: (wsId: string) => ["office", "brief-push", wsId, "bindings"] as const,
};

export function briefPushOptions(wsId: string) {
  return queryOptions({
    queryKey: briefPushKeys.mine(wsId),
    queryFn: () => api.getBriefPushPreference(),
    enabled: !!wsId,
  });
}

export function briefPushBindingsOptions(wsId: string) {
  return queryOptions<BriefPushBinding[]>({
    queryKey: briefPushKeys.bindings(wsId),
    queryFn: () => api.listBriefPushBindings(),
    enabled: !!wsId,
  });
}

/**
 * Saves the caller's preference. Awaits the server, so the form shows what was
 * stored; a refusal (400) carries a message written for the member.
 */
export function useSaveBriefPush(wsId: string) {
  const qc = useQueryClient();
  return useMutation<BriefPushPreference, Error, UpdateBriefPushRequest>({
    mutationFn: (data) => api.updateBriefPushPreference(data),
    onSuccess: (saved) => {
      qc.setQueryData(briefPushKeys.mine(wsId), saved);
    },
  });
}
