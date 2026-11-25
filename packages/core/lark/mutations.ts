import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { LarkGroupSenderPolicy } from "../types";
import { larkKeys } from "./queries";

// Not optimistic: opening group access lets people outside the workspace run
// the agent, so the control reflects only what the server stored.
export function useUpdateLarkGroupSenderPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { installationId: string; policy: LarkGroupSenderPolicy }) =>
      api.updateLarkInstallation(wsId, vars.installationId, {
        group_sender_policy: vars.policy,
      }),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) }),
  });
}
