import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { MergeAgentsEnvResponse } from "../types";
import { workspaceKeys } from "../workspace/queries";

/**
 * Mutation hook for the quick env-injection dialog (MUL-5758): adds or
 * overwrites the given keys across one or more agents, leaving every other key
 * on those agents untouched.
 *
 * Not optimistic. Two of the four conditions in the state rules fail here: the
 * outcome is not locally predictable (only the server knows which submitted
 * keys already existed on each agent, and which agents the caller may write),
 * and the dialog reports a per-agent result the client cannot fabricate.
 *
 * Invalidates the workspace agent list because `custom_env_key_count` — the
 * "N variables configured" indicator — changes for every agent written. The
 * task snapshot and runtime queries are deliberately NOT invalidated: env
 * injection touches neither.
 *
 * @param wsId workspace whose agent list should refresh. Passed in rather than
 *   read from context so the hook stays usable outside a workspace provider.
 */
export function useMergeAgentsEnv(wsId: string) {
  const qc = useQueryClient();
  return useMutation<
    MergeAgentsEnvResponse,
    Error,
    { agentIds: string[]; set: Record<string, string> }
  >({
    mutationFn: ({ agentIds, set }) =>
      api.mergeAgentsEnv({ agent_ids: agentIds, set }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}
