import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { GraphAuthInput } from "../api/codegraph-auth";
export type { GraphCredential, GraphAuthInput } from "../api/codegraph-auth";
export const graphCredentialsOptions = (wsId: string, enabled = true) => ({
  queryKey: ["workspaces", wsId, "git-credentials"],
  queryFn: () => api.listGraphCredentials(wsId),
  enabled,
});
export function useSaveGraphCredential(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...body
    }: {
      id?: string;
      name: string;
      service_url: string;
      token: string;
    }) => api.saveGraphCredential(wsId, body, id),
    onSuccess: () =>
      client.invalidateQueries({
        queryKey: ["workspaces", wsId, "git-credentials"],
      }),
  });
}
export function useDeleteGraphCredential(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteGraphCredential(wsId, id),
    onSuccess: () =>
      client.invalidateQueries({
        queryKey: ["workspaces", wsId, "git-credentials"],
      }),
  });
}
export function useVerifyGraphAccess(wsId: string) {
  return useMutation({
    mutationFn: (body: GraphAuthInput) => api.verifyGraphAccess(wsId, body),
  });
}
export function useUpdateGraphAuth(wsId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: GraphAuthInput & { id: string }) =>
      api.updateGraphAuth(wsId, id, body),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["workspaces", wsId] }),
  });
}
