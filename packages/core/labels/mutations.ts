import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { labelKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
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
    },
  });
}

export function useAttachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.attachLabel(issueId, labelId),
    onSuccess: (data: IssueLabelsResponse) => {
      // Backend may return an empty object when the post-mutation read fails
      // (it logs a warning and skips the broadcast). We only apply the list
      // when the backend gave us one — otherwise rely on onSettled's
      // invalidation to refetch.
      if (data && Array.isArray(data.labels)) {
        qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), data);
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
      qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), (old) =>
        old ? { ...old, labels: old.labels.filter((l: Label) => l.id !== labelId) } : old,
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(labelKeys.byIssue(wsId, issueId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byIssue(wsId, issueId) });
    },
  });
}
