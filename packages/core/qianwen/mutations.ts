import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { qianwenKeys } from "./queries";

export function useInstallQianwenPersonal(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: [...qianwenKeys.all(wsId), "install"] as const,
    mutationFn: ({ agentId }: { agentId: string }) =>
      api.installQianwenPersonal(wsId, agentId),
    gcTime: 0,
    onSettled: () => queryClient.invalidateQueries({
      queryKey: qianwenKeys.installationsRoot(wsId),
    }),
  });
}

export function useMintQianwenPairingCode(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: [...qianwenKeys.all(wsId), "mint-pairing-code"] as const,
    mutationFn: ({ installationId }: { installationId: string }) =>
      api.mintQianwenPairingCode(wsId, installationId),
    gcTime: 0,
    onSuccess: () => queryClient.invalidateQueries({
      queryKey: qianwenKeys.installationsRoot(wsId),
    }),
  });
}

export function useUnbindQianwenCurrentUser(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: [...qianwenKeys.all(wsId), "unbind-current-user"] as const,
    mutationFn: ({ installationId }: { installationId: string }) =>
      api.unbindQianwenCurrentUser(wsId, installationId),
    onSettled: () => queryClient.invalidateQueries({
      queryKey: qianwenKeys.installationsRoot(wsId),
    }),
  });
}

export function useRevokeQianwenInstallation(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: [...qianwenKeys.all(wsId), "revoke-installation"] as const,
    mutationFn: ({ installationId }: { installationId: string }) =>
      api.revokeQianwenInstallation(wsId, installationId),
    onSettled: () => queryClient.invalidateQueries({
      queryKey: qianwenKeys.installationsRoot(wsId),
    }),
  });
}
