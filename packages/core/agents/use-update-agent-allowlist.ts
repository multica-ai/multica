import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { Agent } from "../types";
import { workspaceKeys } from "../workspace/queries";

/**
 * Mutation hook for the creator-only MCP tab: writes an agent's Composio
 * toolkit allowlist via `PUT /api/agents/:id` ({ composio_toolkit_allowlist })
 * — no dedicated endpoint, the existing agent PATCH path carries it (MUL-3870).
 *
 * The hook is optimistic: it patches the matching agent in the cached
 * workspace list before the round-trip so the checkbox flips instantly, then
 * rolls back to the captured snapshot on error and always invalidates on
 * settle so the cache reconverges with the server's normalised slugs
 * (lowercase / trimmed / deduped). The server silently drops the write for
 * non-owners, which is why this is only wired into the owner-gated tab.
 *
 * Accepts the full desired allowlist (`string[]`) — callers compute the next
 * array (add / remove a slug) and pass it wholesale, matching the backend's
 * replace semantics. Pass `[]` to clear every toolkit.
 */
export function useUpdateAgentAllowlist(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation<Agent, Error, string[], { previous?: Agent[] }>({
    mutationFn: (allowlist) =>
      api.updateAgent(agentId, { composio_toolkit_allowlist: allowlist }),
    onMutate: async (allowlist) => {
      const queryKey = workspaceKeys.agents(wsId);
      // Cancel in-flight refetches so they can't clobber the optimistic write.
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<Agent[]>(queryKey);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((a) =>
          a.id === agentId
            ? ({ ...a, composio_toolkit_allowlist: allowlist } as Agent)
            : a,
        ),
      );
      return { previous };
    },
    onError: (_error, _allowlist, context) => {
      if (context?.previous) {
        qc.setQueryData(workspaceKeys.agents(wsId), context.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}
