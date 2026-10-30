import { useCallback } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import type { Agent, UpdateAgentRequest } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { cacheAgentResponse, workspaceKeys } from "@multica/core/workspace/queries";
import { useT } from "../../i18n";

/**
 * Agent field update with an optimistic cache patch, shared by every surface
 * that hosts the agent editors (agent detail page, Team setup tab).
 */
export function useAgentUpdate() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  return useCallback(
    async (id: string, data: Record<string, unknown>) => {
      // Optimistic update: patch the matching agent in the cached list
      // BEFORE the network round-trip so the inspector picker chips flip to
      // the new value immediately on click. Without this, every inspector
      // picker (thinking / visibility / concurrency / model / runtime) waits
      // 0.5-2s for the API response + invalidate + refetch before the trigger
      // updates — readable as obvious lag in the UI.
      //
      // On error we rollback only the fields THIS call wrote, leaving any
      // other concurrently-mutated fields untouched, then invalidate so the
      // cache converges with the server. A whole-list snapshot rollback
      // would clobber a concurrent successful mutation if the failing call
      // resolves last (e.g. flipping visibility then runtime simultaneously
      // and only the visibility PATCH fails).
      const optimisticData =
        typeof data.runtime_id === "string"
          ? { ...data, runtime_bound: data.runtime_id.trim().length > 0 }
          : data;
      const queryKey = workspaceKeys.agents(wsId);
      const detailQueryKey = workspaceKeys.agent(wsId, id);
      const prevAgents = qc.getQueryData<Agent[]>(queryKey);
      const prevListAgent = prevAgents?.find((a) => a.id === id);
      const prevDetailAgent = qc.getQueryData<Agent>(detailQueryKey);
      const previousFields = (previousAgent: Agent | undefined) => {
        const fields: Record<string, unknown> = {};
        if (!previousAgent) return fields;
        for (const key of Object.keys(optimisticData)) {
          fields[key] = (
            previousAgent as unknown as Record<string, unknown>
          )[key];
        }
        return fields;
      };
      const prevListFields = previousFields(prevListAgent);
      const prevDetailFields = previousFields(prevDetailAgent);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((a) =>
          a.id === id ? ({ ...a, ...optimisticData } as Agent) : a,
        ),
      );
      qc.setQueryData<Agent>(detailQueryKey, (old) =>
        old ? ({ ...old, ...optimisticData } as Agent) : old,
      );
      try {
        const updatedAgent = await api.updateAgent(
          id,
          data as UpdateAgentRequest,
        );
        cacheAgentResponse(qc, wsId, updatedAgent, { insertIntoList: false });
        void qc.invalidateQueries({ queryKey });
        toast.success(t(($) => $.detail.agent_updated_toast));
      } catch (e) {
        if (prevListAgent) {
          qc.setQueryData<Agent[]>(queryKey, (old) =>
            old?.map((a) =>
              a.id === id ? ({ ...a, ...prevListFields } as Agent) : a,
            ),
          );
        }
        if (prevDetailAgent) {
          qc.setQueryData<Agent>(detailQueryKey, (old) =>
            old ? ({ ...old, ...prevDetailFields } as Agent) : old,
          );
        }
        void qc.invalidateQueries({ queryKey });
        toast.error(e instanceof Error ? e.message : t(($) => $.detail.update_failed_toast));
        throw e;
      }
    },
    [qc, t, wsId],
  );
}
