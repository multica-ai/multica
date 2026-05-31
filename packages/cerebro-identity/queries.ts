import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import {
  authSettingsSchema,
  EMPTY_AUTH_SETTINGS,
  type CerebroAuthSettings,
} from "./types";

export const authSettingsKeys = {
  byWorkspace: (workspaceId: string) =>
    ["cerebro", "auth-settings", workspaceId] as const,
};

export function authSettingsOptions(workspaceId: string) {
  return queryOptions({
    queryKey: authSettingsKeys.byWorkspace(workspaceId),
    queryFn: async () => {
      const raw = await api.getCerebroAuthSettings(workspaceId);
      return parseWithFallback(
        raw,
        authSettingsSchema,
        { ...EMPTY_AUTH_SETTINGS, workspace_id: workspaceId },
        { endpoint: "GET /api/cerebro/workspaces/:id/auth-settings" },
      );
    },
    enabled: !!workspaceId,
  });
}

export interface UpdateAuthSettingsInput {
  workspaceId: string;
  google_signup_domains: string[];
  default_role: string;
  google_workspace_sync_enabled: boolean;
}

export function useUpdateAuthSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: UpdateAuthSettingsInput) => {
      const raw = await api.updateCerebroAuthSettings(input.workspaceId, {
        google_signup_domains: input.google_signup_domains,
        default_role: input.default_role,
        google_workspace_sync_enabled: input.google_workspace_sync_enabled,
      });
      return parseWithFallback(
        raw,
        authSettingsSchema,
        {
          ...EMPTY_AUTH_SETTINGS,
          workspace_id: input.workspaceId,
          google_signup_domains: input.google_signup_domains,
          default_role: input.default_role,
          google_workspace_sync_enabled: input.google_workspace_sync_enabled,
        },
        { endpoint: "PUT /api/cerebro/workspaces/:id/auth-settings" },
      );
    },
    onSuccess: (settings: CerebroAuthSettings) => {
      qc.setQueryData(
        authSettingsKeys.byWorkspace(settings.workspace_id),
        settings,
      );
    },
  });
}
