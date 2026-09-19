import type { IssueTableGroupDescriptor } from "@multica/core/types";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";

/** Adapt opaque compound cursor keys to the board's node identity. Pagination
 * callbacks still close over the original server keys; never reconstruct them. */
export function workflowLaneBranches(
  branches: IssueGroupBranches,
  lane: IssueTableGroupDescriptor,
  statusFilters: readonly string[],
): IssueGroupBranches {
  const workflowId = lane.value.kind === "workflow" ? lane.value.workflow_id : null;
  const cells = (lane.secondary_groups ?? []).filter(({ value, count }) =>
    value.kind === "workflow_status" && (count > 0 || statusFilters.length === 0 ||
      statusFilters.includes(value.workflow_status_id ?? "") || statusFilters.includes(value.status)),
  );
  const keyOf = (cell: IssueTableGroupDescriptor) => cell.value.kind === "workflow_status"
    ? `workflow_status:${cell.value.workflow_status_id ?? `legacy:${cell.value.status}`}` : cell.key;
  return {
    ...branches,
    descriptors: cells.map((cell) => ({ ...cell, key: keyOf(cell) })),
    pagination: Object.fromEntries(cells.flatMap((cell) => {
      const page = branches.pagination[cell.key];
      return page ? [[keyOf(cell), page]] : [];
    })),
    issues: branches.issues.filter((issue) => (issue.workflow_id ?? null) === workflowId),
    total: lane.count,
    hasMoreGroups: false,
    isError: false,
  };
}
