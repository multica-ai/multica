import type {
  IssueWorkflowStatusNode,
  IssueTableGroupDescriptor,
  IssueTableFacet,
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
  ownWorkflow = false,
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
      workflowStatusIcon: status.icon,
      workflowStatusPhase: status.phase,
      workflowStatusPosition: status.position,
      createData: { workflow_status_id: status.id },
    });
  }

  const workflowIds = new Set([
    ...statuses.map((node) => node.workflow_id),
    ...descriptors.flatMap(({ value }) => value.kind === "workflow_status" && value.workflow_id ? [value.workflow_id] : []),
  ]);
  for (const descriptor of descriptors) {
    const value = descriptor.value;
    if (value.kind !== "workflow_status") continue;
    const id = descriptor.key;
    const existing = groups.get(id);
    groups.set(id, {
      id,
      workflowStatusId: value.workflow_status_id ?? null,
      workflowId: value.workflow_id,
      workflowName: value.workflow_name,
      workflowDefault: value.is_default,
      workflowStatusLegacyKey: value.status || undefined,
      workflowStatusColor: value.color,
      workflowStatusIcon: value.icon,
      workflowStatusPhase: value.phase,
      workflowStatusPosition: value.position,
      workflowStatusArchived: value.archived,
      workflowStatusHistorical: !ownWorkflow && existing === undefined,
      createData: ownWorkflow && value.workflow_status_id && !value.archived
        ? { workflow_status_id: value.workflow_status_id } : undefined,
      ...existing,
      title: workflowIds.size > 1 && value.workflow_name && !value.is_default ? `${value.workflow_name} / ${value.name}` : value.name,
      totalCount: descriptor.count,
    });
  }

  return Array.from(groups.values()).toSorted((a, b) => {
    if (!!a.workflowStatusHistorical !== !!b.workflowStatusHistorical) {
      return a.workflowStatusHistorical ? 1 : -1;
    }
    if (a.workflowId !== b.workflowId) {
      if (!!a.workflowDefault !== !!b.workflowDefault) return a.workflowDefault ? -1 : 1;
      const order = (a.workflowName ?? "").localeCompare(b.workflowName ?? "") || (a.workflowId ?? "").localeCompare(b.workflowId ?? "");
      if (order) return order;
    }
    const aPosition = a.workflowStatusPosition ?? Number.MAX_SAFE_INTEGER;
    const bPosition = b.workflowStatusPosition ?? Number.MAX_SAFE_INTEGER;
    if (aPosition !== bPosition) return aPosition - bPosition;
    return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
  });
}

/** Preserve counts for legacy saved filters while offering exact node choices. */
export function workflowFacetForDisplay(facet: IssueTableFacet, legacyFacet?: IssueTableFacet): IssueTableFacet {
  if (facet.kind !== "workflow_status") return facet;
  const legacyCounts = new Map<string, number>();
  for (const value of facet.values) {
    const key = value.status_node?.status || (value.key.startsWith("legacy:") ? value.key.slice(7) : undefined);
    if (key) legacyCounts.set(key, (legacyCounts.get(key) ?? 0) + value.count);
  }
  return { ...facet, kind: "status", values: [
    ...facet.values,
    ...(legacyFacet?.values ?? Array.from(legacyCounts, ([key, count]) => ({ key, count }))),
  ] };
}
