import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { gitlabKeys } from "./queries";
import type { ConnectGitlabInput, GitlabConnection } from "./types";

export function useConnectWorkspaceGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation<GitlabConnection, Error, ConnectGitlabInput>({
    mutationFn: (input) => api.connectWorkspaceGitlab(wsId, input),
    onSuccess: (data) => {
      qc.setQueryData(gitlabKeys.connection(wsId), data);
    },
  });
}

export function useDisconnectWorkspaceGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.disconnectWorkspaceGitlab(wsId),
    onSuccess: () => {
      qc.removeQueries({ queryKey: gitlabKeys.connection(wsId) });
    },
  });
}
