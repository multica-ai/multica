import type {
  IssueWorkflowStatusNode,
  IssueTableGroupDescriptor,
} from "@multica/core/types";
import type { BoardColumnGroup } from "../components/board-column";

export function workflowStatusGroupId(statusId: string): string {
  return `workflow_status:${statusId}`;
}

/**
 * Merge the project's current definition (which owns empty columns and order)
 * with server descriptors for pinned historical definitions that still own
 * issues. Identity is always the Status Node id; names are display-only and
 * therefore two workflows may safely contain the same label.
 */
export function buildWorkflowStatusGroups(
  statuses: readonly IssueWorkflowStatusNode[],
  descriptors: readonly IssueTableGroupDescriptor[],
): BoardColumnGroup[] {
  const groups = new Map<string, BoardColumnGroup>();
  for (const status of statuses) {
    if (status.archived_at) continue;
    const id = workflowStatusGroupId(status.id);
    groups.set(id, {
      id,
      title: status.name,
      workflowStatusId: status.id,
      workflowId: status.workflow_id,
      workflowStatusLegacyKey: status.legacy_status_key ?? undefined,
      workflowStatusColor: status.color,
      workflowStatusPosition: status.position,
      createData: { workflow_status_id: status.id },
    });
  }

  for (const descriptor of descriptors) {
    const value = descriptor.value;
    if (value.kind !== "workflow_status") continue;
    const id = descriptor.key;
    const existing = groups.get(id);
    groups.set(id, {
      id,
      title: value.name,
      workflowStatusId: value.workflow_status_id ?? null,
      workflowId: value.workflow_id,
      workflowStatusLegacyKey: value.status || undefined,
      workflowStatusColor: value.color,
      workflowStatusPosition: value.position,
      workflowStatusArchived: value.archived,
      workflowStatusHistorical: existing === undefined,
      createData: undefined,
      ...existing,
      totalCount: descriptor.count,
    });
  }

  return Array.from(groups.values()).toSorted((a, b) => {
    if (!!a.workflowStatusHistorical !== !!b.workflowStatusHistorical) {
      return a.workflowStatusHistorical ? 1 : -1;
    }
    const aPosition = a.workflowStatusPosition ?? Number.MAX_SAFE_INTEGER;
    const bPosition = b.workflowStatusPosition ?? Number.MAX_SAFE_INTEGER;
    if (aPosition !== bPosition) return aPosition - bPosition;
    return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
  });
}
