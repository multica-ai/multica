import { useQueryClient, useMutation } from "@tanstack/react-query";
import { api } from "../api";
import { gitlabKeys } from "./queries";

export function useDeleteGitLabConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) => api.deleteGitLabConnection(wsId, connectionId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.connections(wsId) });
    },
  });
}
