import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { labelKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import {
  invalidateIssueLabelDerivatives,
  onIssueLabelsChanged,
  patchIssueLabels,
} from "../issues/ws-updaters";
import type {
  Label,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  IssueLabelsResponse,
  LabelResourceType,
  ResourceLabelsResponse,
} from "../types";

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateLabelRequest) => api.createLabel(data),
    onSuccess: (label) => {
      qc.setQueryData<ListLabelsResponse>(labelKeys.list(wsId, label.resource_type ?? "issue"), (old) =>
        old && !old.labels.some((l) => l.id === label.id)
          ? { ...old, labels: [...old.labels, label], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
    },
  });
}

/**
 * Optimistic rename/recolor. Matches the useUpdateProject pattern: apply the
 * change locally, snapshot for rollback, invalidate on settle. Without this
 * the UI freezes for the round-trip on every edit.
 */
export function useUpdateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, resource_type: _resourceType, ...data }: { id: string; resource_type?: LabelResourceType } & UpdateLabelRequest) =>
      api.updateLabel(id, data),
    onMutate: async ({ id, resource_type = "issue", ...data }) => {
      const listKey = labelKeys.list(wsId, resource_type);
      await qc.cancelQueries({ queryKey: listKey });
      const prevList = qc.getQueryData<ListLabelsResponse>(listKey);
      qc.setQueryData<ListLabelsResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              labels: old.labels.map((l) => (l.id === id ? { ...l, ...data } : l)),
            }
          : old,
      );
      return { prevList, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.prevList);
    },
    onSettled: () => {
      // Invalidate the entire labels scope so any byIssue cache holding a
      // stale copy of this label is refetched. The list cache is the source
      // of truth; byIssue views will re-render with the fresh data.
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      // Issues now embed labels (denormalized snapshot), so a rename/recolor
      // also has to refresh the issues caches that hold those snapshots.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useDeleteLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: string | { id: string; resource_type: LabelResourceType }) =>
      api.deleteLabel(typeof input === "string" ? input : input.id),
    onMutate: async (input) => {
      const id = typeof input === "string" ? input : input.id;
      const resourceType = typeof input === "string" ? "issue" : input.resource_type;
      const listKey = labelKeys.list(wsId, resourceType);
      await qc.cancelQueries({ queryKey: listKey });
      const prev = qc.getQueryData<ListLabelsResponse>(listKey);
      qc.setQueryData<ListLabelsResponse>(listKey, (old) =>
        old
          ? { ...old, labels: old.labels.filter((l) => l.id !== id), total: old.total - 1 }
          : old,
      );
      return { prev, listKey };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      // A deleted label still lives in cached issue.labels arrays until we
      // refetch — invalidate so list/board chips drop the orphan.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useAttachResourceLabel(
  resourceType: "agent" | "skill",
  resourceId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) =>
      api.attachLabelToResource(resourceType, resourceId, labelId),
    onSuccess: (data: ResourceLabelsResponse) => {
      qc.setQueryData(
        labelKeys.byResource(wsId, resourceType, resourceId),
        data,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeysForLabels(resourceType, wsId) });
    },
  });
}

export function useDetachResourceLabel(
  resourceType: "agent" | "skill",
  resourceId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) =>
      api.detachLabelFromResource(resourceType, resourceId, labelId),
    onSuccess: (data: ResourceLabelsResponse) => {
      qc.setQueryData(
        labelKeys.byResource(wsId, resourceType, resourceId),
        data,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeysForLabels(resourceType, wsId) });
    },
  });
}

function workspaceKeysForLabels(resourceType: "agent" | "skill", wsId: string) {
  return ["workspaces", wsId, resourceType === "agent" ? "agents" : "skills"] as const;
}

export function useAttachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.attachLabel(issueId, labelId),
    onMutate: async (labelId) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: labelKeys.byIssue(wsId, issueId) }),
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
      ]);
      const prev = qc.getQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId));
      // Only patch when we already know the current label set — otherwise
      // appending `[label]` to an empty array would wipe denormalized
      // labels in issue list/detail caches and rollback couldn't restore
      // them. If byIssue isn't cached yet (user clicked before the picker
      // fetched), skip the optimistic patch and rely on onSettled refetch.
      if (!prev) return { prev };
      if (prev.labels.some((l) => l.id === labelId)) return { prev };
      const list = qc.getQueryData<ListLabelsResponse>(labelKeys.list(wsId));
      const label = list?.labels.find((l) => l.id === labelId);
      if (!label) return { prev };
      const next: IssueLabelsResponse = { ...prev, labels: [...prev.labels, label] };
      qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), next);
      patchIssueLabels(qc, wsId, issueId, next.labels);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byIssue(wsId, issueId), ctx.prev);
        patchIssueLabels(qc, wsId, issueId, ctx.prev.labels);
      }
    },
    onSuccess: (data: IssueLabelsResponse) => {
      // Backend may return an empty object when the post-mutation read fails
      // (it logs a warning and skips the broadcast). Only apply the list
      // when the backend gave us one — otherwise the optimistic patch from
      // onMutate stands until onSettled's invalidation refetches.
      if (data && Array.isArray(data.labels)) {
        qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), data);
        onIssueLabelsChanged(qc, wsId, issueId, data.labels);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
      invalidateIssueLabelDerivatives(qc, wsId);
    },
  });
}

/**
 * Attach a label to an issue identified by mutation variables rather than a
 * closed-over `issueId`. `useAttachLabel` binds one issueId at hook-call time
 * for the optimistic issue-detail flow; this variant defers the id to call
 * time so a caller can attach labels to an issue that doesn't exist yet at
 * render — e.g. labels chosen in the create-issue dialog, attached right after
 * the issue is created. No optimistic patch: the create flow closes the dialog
 * and relies on the settle-time invalidation to surface the new labels.
 */
export function useAttachLabelToIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, labelId }: { issueId: string; labelId: string }) =>
      api.attachLabel(issueId, labelId),
    onSettled: (_data, _err, { issueId }) => {
      qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
      // Issues embed a denormalized labels snapshot, so refresh the issues
      // caches that hold it (list / board / detail) once the attach settles.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useDetachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.detachLabel(issueId, labelId),
    onMutate: async (labelId) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: labelKeys.byIssue(wsId, issueId) }),
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
      ]);
      const prev = qc.getQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId));
      const next = prev
        ? { ...prev, labels: prev.labels.filter((l: Label) => l.id !== labelId) }
        : undefined;
      if (next) {
        qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), next);
        patchIssueLabels(qc, wsId, issueId, next.labels);
      }
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byIssue(wsId, issueId), ctx.prev);
        patchIssueLabels(qc, wsId, issueId, ctx.prev.labels);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
      invalidateIssueLabelDerivatives(qc, wsId);
    },
  });
}
