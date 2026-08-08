import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "../runtimes/queries";
import { workspaceKeys } from "../workspace/queries";
import { extensionKeys } from "./queries";

export function useImportPlatformExtension(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (document: Uint8Array | ArrayBuffer) =>
      api.importPlatformExtension(document),
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
