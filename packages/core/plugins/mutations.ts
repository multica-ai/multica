import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { PluginConfigRequest, PluginInstallRequest, PluginPreviewRequest } from "../types";
import { pluginKeys } from "./queries";

function useInvalidatePlugins(wsId: string) {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: pluginKeys.all(wsId) });
}

/**
 * Preview is deliberately a mutation, not a query: it performs an outbound
 * fetch of a URL the administrator just typed, so it must run on an explicit
 * action and never be replayed by a cache refetch.
 */
export function usePreviewPlugin(wsId: string) {
  return useMutation({
    mutationFn: (request: PluginPreviewRequest) => api.previewPlugin(wsId, request),
  });
}

export function useInstallPlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: (request: PluginInstallRequest) => api.installPlugin(wsId, request),
    onSettled: invalidate,
  });
}

export function useConfigurePlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, ...request }: PluginConfigRequest & { installationId: string }) =>
      api.configurePlugin(wsId, installationId, request),
    onSettled: invalidate,
  });
}

export function useSetPluginEnabled(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, enabled }: { installationId: string; enabled: boolean }) =>
      api.setPluginEnabled(wsId, installationId, enabled),
    onSettled: invalidate,
  });
}

export function useUninstallPlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: (installationId: string) => api.uninstallPlugin(wsId, installationId),
    onSettled: invalidate,
  });
}
