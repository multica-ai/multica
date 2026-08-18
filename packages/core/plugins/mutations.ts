import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { PluginBindingRequest, PluginReleaseRequest, RemoteMCPConfigRequest, RemoteMCPOAuthStartRequest } from "../types";
import { pluginKeys } from "./queries";

function useInvalidatePlugins(wsId: string) {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: pluginKeys.all(wsId) });
}

export function useInstallPlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: (request: PluginReleaseRequest) => api.installPlugin(wsId, request),
    onSettled: invalidate,
  });
}

export function useUpgradePlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, ...request }: PluginReleaseRequest & { installationId: string }) =>
      api.upgradePlugin(wsId, installationId, request),
    onSettled: invalidate,
  });
}

export function useSetPluginEnabled(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, enabled, binding }: {
      installationId: string;
      enabled: boolean;
      binding: PluginBindingRequest;
    }) => api.setPluginEnabled(wsId, installationId, enabled, binding),
    onSettled: invalidate,
  });
}

export function useRollbackPlugin(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, version }: { installationId: string; version: string }) =>
      api.rollbackPlugin(wsId, installationId, version),
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

export function useConfigurePluginRemoteMCP(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, contributionKey, request }: { installationId: string; contributionKey: string; request: RemoteMCPConfigRequest }) =>
      api.configurePluginRemoteMCP(wsId, installationId, contributionKey, request),
    onSettled: invalidate,
  });
}

export function useTestPluginRemoteMCP(wsId: string) {
  return useMutation({
    mutationFn: ({ installationId, contributionKey }: { installationId: string; contributionKey: string }) =>
      api.testPluginRemoteMCP(wsId, installationId, contributionKey),
  });
}

export function useStartPluginRemoteMCPOAuth(wsId: string) {
  return useMutation({
    mutationFn: ({ installationId, contributionKey, request }: { installationId: string; contributionKey: string; request: RemoteMCPOAuthStartRequest }) =>
      api.startPluginRemoteMCPOAuth(wsId, installationId, contributionKey, request),
  });
}

export function useApprovePluginRemoteMCPTools(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, contributionKey, tools }: { installationId: string; contributionKey: string; tools: string[] }) =>
      api.approvePluginRemoteMCPTools(wsId, installationId, contributionKey, tools),
    onSettled: invalidate,
  });
}

export function useRevokePluginRemoteMCPCredential(wsId: string) {
  const invalidate = useInvalidatePlugins(wsId);
  return useMutation({
    mutationFn: ({ installationId, contributionKey }: { installationId: string; contributionKey: string }) =>
      api.revokePluginRemoteMCPCredential(wsId, installationId, contributionKey),
    onSettled: invalidate,
  });
}
