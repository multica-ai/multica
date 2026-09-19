"use client";

import { createContext, useContext, useMemo } from "react";
import type {
  IssueWorkflowStatusNode,
  IssueStatusEntry,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
} from "@multica/core/types";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  buildIssueStatusCatalog,
  normalizeIssueStatusCategory,
} from "@multica/core/issue-statuses/queries";

export const SurfaceWorkflowContext = createContext<{
  statuses?: IssueWorkflowStatusNode[];
  groups?: IssueTableGroupDescriptor[];
  facets?: IssueTableFacetsResponse;
}>({});

export function useSurfaceWorkflow() {
  return useContext(SurfaceWorkflowContext);
}

export function workflowColumnKey(group: IssueTableGroupDescriptor): string | undefined {
  if (group.value.kind === "workflow_status") {
    return group.value.workflow_status_id ?? group.value.status;
  }
  return group.value.kind === "status" ? group.value.status : undefined;
}

export function useSurfaceStatusCatalog(wsId: string) {
  const workspace = useIssueStatuses(wsId);
  const { statuses, groups, facets } = useSurfaceWorkflow();
  return useMemo(() => {
    if (!statuses) return workspace;
    const entries = new Map<string, IssueStatusEntry>(statuses.map((node) => [node.id, {
      ...node,
      workspace_id: wsId,
      key: node.id,
      category: normalizeIssueStatusCategory(node.phase) ?? "unstarted",
      is_system: false,
    }]));
    // Keep legacy saved-view filters readable alongside exact node filters.
    for (const legacy of workspace.statuses) if (!entries.has(legacy.key)) entries.set(legacy.key, legacy);
    const facetGroups: IssueTableGroupDescriptor[] = (facets?.facets ?? []).flatMap((facet) =>
      facet.values.flatMap((value) => value.status_node ? [{ key: value.key, count: value.count, value: value.status_node }] : []),
    );
    const workflowIds = new Set(statuses.map((node) => node.workflow_id));
    for (const group of [...(groups ?? []), ...facetGroups]) {
      for (const item of group.secondary_groups ?? [group]) {
        if (item.value.kind === "workflow_status" && item.value.workflow_id) workflowIds.add(item.value.workflow_id);
      }
    }
    for (const group of [...(groups ?? []), ...facetGroups]) {
      for (const item of group.secondary_groups ?? [group]) {
        const value = item.value;
        if (value.kind !== "workflow_status") continue;
        if (!value.workflow_status_id) {
          const legacy = workspace.entryOf(value.status);
          if (legacy) entries.set(legacy.key, legacy);
          continue;
        }

        // Pinned definitions remain readable after project defaults change;
        // only archived nodes are unavailable as transition targets.
        entries.set(value.workflow_status_id, {
          id: value.workflow_status_id, workspace_id: wsId, key: value.workflow_status_id,
          name: workflowIds.size > 1 && value.workflow_name && !value.is_default ? `${value.workflow_name} / ${value.name}` : value.name, description: "", category: normalizeIssueStatusCategory(value.phase ?? "unstarted") ?? "unstarted",
          color: value.color ?? "#808080", icon: value.icon, position: value.position ?? 0,
          is_system: false, archived_at: value.archived ? "archived" : null, created_at: "", updated_at: "",
        });
      }
    }
    // Keep legacy keys for saved filters, but order concrete nodes by workflow
    // and then node position rather than interleaving different definitions.
    const order = new Map<string, string>();
    for (const group of [...(groups ?? []), ...facetGroups]) for (const item of group.secondary_groups ?? [group]) {
      const node = item.value;
      if (node.kind === "workflow_status" && node.workflow_status_id) order.set(node.workflow_status_id,
        `${node.is_default ? "0" : "1"}:${node.workflow_name ?? ""}:${node.workflow_id ?? ""}`);
    }
    return buildIssueStatusCatalog([...entries.values()].sort((a, b) =>
      (order.get(a.key) ?? "0").localeCompare(order.get(b.key) ?? "0") || a.position - b.position));
  }, [groups, facets, statuses, workspace, wsId]);
}

/** Concrete drag/create targets are resolved independently of their labels. */
export function useSurfaceNodeWorkflows() {
  const { statuses, groups, facets } = useSurfaceWorkflow();
  return useMemo(() => {
    const nodes = new Map((statuses ?? []).map((node) => [node.id, node.workflow_id]));
    const descriptors = [...(groups ?? []), ...(facets?.facets ?? []).flatMap((facet) => facet.values.flatMap((value) =>
      value.status_node ? [{ value: value.status_node }] : []))];
    for (const descriptor of descriptors) {
      const children = "secondary_groups" in descriptor ? descriptor.secondary_groups : undefined;
      for (const { value } of children ?? [descriptor]) {
        if (value.kind === "workflow_status" && value.workflow_status_id && value.workflow_id) nodes.set(value.workflow_status_id, value.workflow_id);
      }
    }
    return nodes;
  }, [statuses, groups, facets]);
}
