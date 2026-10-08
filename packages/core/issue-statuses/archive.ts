import { z } from "zod";
import { ApiError } from "../api/client";
import { createStore } from "zustand/vanilla";
import { viewStoreSlice, type IssueViewState } from "../issues/stores/view-store";

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
export function createIssueStatusListStore(key: string) {
  return createStore<IssueViewState>()((set) => ({
    ...viewStoreSlice(set),
    viewMode: "list", grouping: "status", statusFilters: [key],
    priorityFilters: [], assigneeFilters: [], includeNoAssignee: false,
    creatorFilters: [], projectFilters: [], includeNoProject: false,
    labelFilters: [], propertyFilters: {}, dateFilter: null,
    agentRunningFilter: false, showSubIssues: true,
    hiddenStatuses: [], listCollapsedStatuses: [],
  }));
}
