import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "../runtimes/queries";
import { workspaceKeys } from "../workspace/queries";
import { extensionKeys } from "./queries";
import type { PlatformExtensionImportConfiguration } from "./types";

export function usePreviewPlatformExtension() {
  return useMutation({
    mutationFn: (document: Uint8Array | ArrayBuffer) =>
      api.previewPlatformExtension(document),
  });
}

export function useImportPlatformExtension(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      document: Uint8Array | ArrayBuffer;
      configuration?: PlatformExtensionImportConfiguration;
    } | Uint8Array | ArrayBuffer) => {
      const request = input instanceof Uint8Array || input instanceof ArrayBuffer
        ? { document: input, configuration: undefined }
        : input;
      return api.importPlatformExtension(request.document, request.configuration);
    },
    onSuccess: async () => {
      for (const queryKey of [
        extensionKeys.all(wsId),
        workspaceKeys.agents(wsId),
        workspaceKeys.skills(wsId),
        workspaceKeys.squads(wsId),
        runtimeKeys.all(wsId),
      ]) {
        await queryClient.invalidateQueries({ queryKey });
      }
    },
  });
}

export function useUpdatePlatformExtension(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; configuration: PlatformExtensionImportConfiguration }) =>
      api.updatePlatformExtension(input.id, input.configuration),
    onSuccess: async (_result, input) => {
      for (const queryKey of [
        extensionKeys.all(wsId),
        extensionKeys.detail(wsId, input.id),
        workspaceKeys.agents(wsId),
        workspaceKeys.squads(wsId),
        runtimeKeys.all(wsId),
      ]) {
        await queryClient.invalidateQueries({ queryKey });
      }
    },
  });
}

export function useArchivePlatformExtension(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.archivePlatformExtension(id),
    onSuccess: async (_result, id) => {
      for (const queryKey of [
        extensionKeys.all(wsId),
        extensionKeys.detail(wsId, id),
        workspaceKeys.squads(wsId),
      ]) {
        await queryClient.invalidateQueries({ queryKey });
      }
    },
  });
}
