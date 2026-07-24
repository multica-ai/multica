/**
 * Artifact queries — mobile-owned. The only consumer today is the Workpad
 * panel (FIR-3765), which needs the issue's `kind:"plan"` artifact. Mirrors
 * web's `artifactsByIssueOptions` (packages/cerebro-artifacts/core/queries.ts)
 * but selects the plan here so the panel receives a single plan or null.
 *
 * Key shape follows the 3-segment factory convention (apps/mobile/CLAUDE.md
 * "Query / mutation factory pattern") so a workspace can be invalidated by the
 * `.all` prefix.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { selectPlanArtifact } from "@/lib/workpad";

export const artifactKeys = {
  all: (wsId: string | null) => ["artifacts", wsId] as const,
  byIssue: (wsId: string | null, issueId: string) =>
    [...artifactKeys.all(wsId), "issue", issueId] as const,
};

// issuePlanOptions resolves the issue's Workpad plan: fetch the coupled
// artifacts, then select the kind:"plan" one (most recently updated wins).
// Returns null when the issue has no plan — the panel renders nothing then.
export const issuePlanOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: artifactKeys.byIssue(wsId, issueId),
    queryFn: async ({ signal }) => {
      const artifacts = await api.listArtifactsByIssue(issueId, { signal });
      return selectPlanArtifact(artifacts);
    },
    enabled: !!wsId && !!issueId,
  });
