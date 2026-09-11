import { z } from "zod";
import { ApiError } from "../api/client";
import { useActiveIssueViewStore, issueViewContainerKey } from "../issue-views/active-view-store";
import { useIssuesScopeStore } from "../issues/stores/issues-scope-store";
import { getIssueSurfaceViewStore } from "../issues/stores/surface-view-store";

const inUseErrorSchema = z.object({
  code: z.literal("issue_status_in_use"),
  issue_count: z.number().int().positive(),
});

/** Parse the archive conflict without trusting an old/malformed server body. */
export function issueStatusArchiveConflictCount(error: unknown): number | null {
  if (!(error instanceof ApiError) || error.status !== 409) return null;
  const parsed = inUseErrorSchema.safeParse(error.body);
  return parsed.success ? parsed.data.issue_count : null;
}

/** Open the full exact-status list, not a previously filtered or saved view. */
export function prepareIssueStatusList(wsId: string, key: string): void {
  useActiveIssueViewStore.getState().setActive(issueViewContainerKey(wsId, { scope_type: "workspace" }), null);
  useIssuesScopeStore.getState().setScope("issues", "all");
  getIssueSurfaceViewStore("workspace:all").setState({
    viewMode: "list", grouping: "status", statusFilters: [key],
    priorityFilters: [], assigneeFilters: [], includeNoAssignee: false,
    creatorFilters: [], projectFilters: [], includeNoProject: false,
    labelFilters: [], propertyFilters: {}, dateFilter: null,
    agentRunningFilter: false, showSubIssues: true,
    hiddenStatuses: [], listCollapsedStatuses: [],
  });
}
