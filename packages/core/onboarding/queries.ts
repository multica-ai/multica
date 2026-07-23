import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";

/**
 * Count of issues in the workspace completed by an AI assignee (agent
 * or squad). Sole consumer is the source-backfill gate: the prompt
 * waits until the user has watched agents finish real work before
 * asking the attribution question (SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES
 * in `needs-backfill.ts`).
 *
 * `limit: 1` because only `total` matters — the issue rows themselves
 * are discarded. Keyed under `issueKeys.all(wsId)` on purpose: issue
 * mutations and realtime events invalidate that prefix, so the count
 * refreshes as agents complete work and the prompt can appear without
 * a reload. The query is expected to be `enabled` only for the small
 * cohort that still owes a source answer, so the extra refetches don't
 * follow users around forever.
 */
export function agentCompletedIssueCountOptions(wsId: string) {
  return queryOptions({
    queryKey: [...issueKeys.all(wsId), "agent-done-count"] as const,
    queryFn: async () => {
      const res = await api.listIssues({
        workspace_id: wsId,
        statuses: ["done"],
        assignee_types: ["agent", "squad"],
        limit: 1,
      });
      return res.total;
    },
    staleTime: 30_000,
  });
}
