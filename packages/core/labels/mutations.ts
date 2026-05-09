import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { labelKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { onIssueLabelsChanged } from "../issues/ws-updaters";
import type {
  Label,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  IssueLabelsResponse,
} from "../types";

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateLabelRequest) => api.createLabel(data),
    onSuccess: (label) => {
      qc.setQueryData<ListLabelsResponse>(labelKeys.list(wsId), (old) =>
        old && !old.labels.some((l) => l.id === label.id)
          ? { ...old, labels: [...old.labels, label], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.list(wsId) });
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
    mutationFn: ({ id, ...data }: { id: string } & UpdateLabelRequest) =>
      api.updateLabel(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: labelKeys.list(wsId) });
      const prevList = qc.getQueryData<ListLabelsResponse>(labelKeys.list(wsId));
      qc.setQueryData<ListLabelsResponse>(labelKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              labels: old.labels.map((l) => (l.id === id ? { ...l, ...data } : l)),
            }
          : old,
      );
      return { prevList, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(labelKeys.list(wsId), ctx.prevList);
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
    mutationFn: (id: string) => api.deleteLabel(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: labelKeys.list(wsId) });
      const prev = qc.getQueryData<ListLabelsResponse>(labelKeys.list(wsId));
      qc.setQueryData<ListLabelsResponse>(labelKeys.list(wsId), (old) =>
        old
          ? { ...old, labels: old.labels.filter((l) => l.id !== id), total: old.total - 1 }
          : old,
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(labelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      // A deleted label still lives in cached issue.labels arrays until we
      // refetch — invalidate so list/board chips drop the orphan.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useAttachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.attachLabel(issueId, labelId),
    onMutate: async (labelId) => {
      await qc.cancelQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
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
      onIssueLabelsChanged(qc, wsId, issueId, next.labels);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byIssue(wsId, issueId), ctx.prev);
        onIssueLabelsChanged(qc, wsId, issueId, ctx.prev.labels);
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
    },
  });
}

export function useDetachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.detachLabel(issueId, labelId),
    onMutate: async (labelId) => {
      await qc.cancelQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
      const prev = qc.getQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId));
      const next = prev
        ? { ...prev, labels: prev.labels.filter((l: Label) => l.id !== labelId) }
        : undefined;
      if (next) {
        qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), next);
        onIssueLabelsChanged(qc, wsId, issueId, next.labels);
      }
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byIssue(wsId, issueId), ctx.prev);
        onIssueLabelsChanged(qc, wsId, issueId, ctx.prev.labels);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
    },
  });
}
