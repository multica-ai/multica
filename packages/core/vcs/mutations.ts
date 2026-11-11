import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { vcsKeys } from "./queries";

export function useUpdateVCSConnectionToken(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: ({ connectionId, accessToken }: { connectionId: string; accessToken: string }) =>
      api.updateVCSConnectionToken(wsId, connectionId, { access_token: accessToken }),
    onSuccess: () => qc.invalidateQueries({ queryKey: vcsKeys.all(wsId) }),
  });
}
