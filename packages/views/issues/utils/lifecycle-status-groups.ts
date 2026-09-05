import type {
  IssueLifecycleStatusNode,
  IssueTableGroupDescriptor,
} from "@multica/core/types";
import type { BoardColumnGroup } from "../components/board-column";

export function lifecycleStatusGroupId(statusId: string): string {
  return `lifecycle_status:${statusId}`;
}

/**
 * Merge the project's current definition (which owns empty columns and order)
 * with server descriptors for pinned historical definitions that still own
 * issues. Identity is always the Status Node id; names are display-only and
 * therefore two lifecycles may safely contain the same label.
 */
export function buildLifecycleStatusGroups(
  statuses: readonly IssueLifecycleStatusNode[],
  descriptors: readonly IssueTableGroupDescriptor[],
): BoardColumnGroup[] {
  const groups = new Map<string, BoardColumnGroup>();
  for (const status of statuses) {
    if (status.archived_at) continue;
    const id = lifecycleStatusGroupId(status.id);
    groups.set(id, {
      id,
      title: status.name,
      lifecycleStatusId: status.id,
      lifecycleId: status.lifecycle_id,
      lifecycleStatusLegacyKey: status.legacy_status_key ?? undefined,
      lifecycleStatusColor: status.color,
      lifecycleStatusPosition: status.position,
      createData: status.legacy_status_key
        ? { status: status.legacy_status_key }
        : undefined,
    });
  }

  for (const descriptor of descriptors) {
    const value = descriptor.value;
    if (value.kind !== "lifecycle_status") continue;
    const id = descriptor.key;
    const existing = groups.get(id);
    groups.set(id, {
      id,
      title: value.name,
      lifecycleStatusId: value.lifecycle_status_id ?? null,
      lifecycleId: value.lifecycle_id,
      lifecycleStatusLegacyKey: value.status || undefined,
      lifecycleStatusColor: value.color,
      lifecycleStatusPosition: value.position,
      lifecycleStatusArchived: value.archived,
      lifecycleStatusHistorical: existing === undefined,
      createData: undefined,
      ...existing,
      totalCount: descriptor.count,
    });
  }

  return Array.from(groups.values()).toSorted((a, b) => {
    if (!!a.lifecycleStatusHistorical !== !!b.lifecycleStatusHistorical) {
      return a.lifecycleStatusHistorical ? 1 : -1;
    }
    const aPosition = a.lifecycleStatusPosition ?? Number.MAX_SAFE_INTEGER;
    const bPosition = b.lifecycleStatusPosition ?? Number.MAX_SAFE_INTEGER;
    if (aPosition !== bPosition) return aPosition - bPosition;
    return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
  });
}
