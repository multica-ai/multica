import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import {
  authSettingsSchema,
  cerebroGroupPickListSchema,
  EMPTY_AUTH_SETTINGS,
  type CerebroAuthSettings,
  type CerebroGroupPick,
} from "./types";

export const authSettingsKeys = {
  byWorkspace: (workspaceId: string) =>
    ["cerebro", "auth-settings", workspaceId] as const,
};

export const authSettingsGroupKeys = {
  list: (workspaceId: string) =>
    ["cerebro", "auth-settings", workspaceId, "groups"] as const,
};

const EMPTY_GROUP_PICKS: CerebroGroupPick[] = [];

/** Workspace groups for the default-group picker — uses core API only (no cerebro-groups import). */
export function authSettingsGroupListOptions(workspaceId: string) {
  return queryOptions({
    queryKey: authSettingsGroupKeys.list(workspaceId),
    queryFn: async (): Promise<CerebroGroupPick[]> => {
      const raw = await api.listCerebroGroups<unknown>(workspaceId);
      return parseWithFallback(raw, cerebroGroupPickListSchema, EMPTY_GROUP_PICKS, {
        endpoint: "listCerebroGroups",
      });
    },
    enabled: !!workspaceId,
  });
}

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
  default_group_id: string | null;
  google_workspace_sync_enabled: boolean;
}

export function useUpdateAuthSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: UpdateAuthSettingsInput) => {
      const raw = await api.updateCerebroAuthSettings(input.workspaceId, {
        google_signup_domains: input.google_signup_domains,
        default_role: input.default_role,
        default_group_id: input.default_group_id,
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
          default_group_id: input.default_group_id,
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
