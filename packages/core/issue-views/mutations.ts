import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateIssueViewRequest,
  DuplicateIssueViewRequest,
  IssueView,
  IssueViewScopeInput,
  SetDefaultIssueViewRequest,
  UpdateIssueViewRequest,
} from "../types";
import { issueViewKeys } from "./queries";

function useInvalidateIssueViews() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return (view?: IssueView) => {
    if (view) qc.setQueryData(issueViewKeys.detail(wsId, view.id), view);
    return qc.invalidateQueries({ queryKey: issueViewKeys.all(wsId) });
  };
}

export function useCreateIssueView() {
  const invalidate = useInvalidateIssueViews();
  return useMutation({
    mutationFn: (data: CreateIssueViewRequest) => api.createIssueView(data),
    onSuccess: invalidate,
  });
}

export function useUpdateIssueView() {
  const invalidate = useInvalidateIssueViews();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateIssueViewRequest }) =>
      api.updateIssueView(id, data),
    onSuccess: invalidate,
  });
}

export function useDeleteIssueView() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueView(id),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: issueViewKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: issueViewKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: ["pins", wsId] });
    },
  });
}

export function useDuplicateIssueView() {
  const invalidate = useInvalidateIssueViews();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: DuplicateIssueViewRequest }) =>
      api.duplicateIssueView(id, data),
    onSuccess: invalidate,
  });
}

export function useSetDefaultIssueView() {
  const invalidate = useInvalidateIssueViews();
  return useMutation({
    mutationFn: (data: SetDefaultIssueViewRequest) => api.setDefaultIssueView(data),
    onSuccess: () => invalidate(),
  });
}

export function defaultIssueViewRequest(
  scope: IssueViewScopeInput,
  viewId: string | null,
): SetDefaultIssueViewRequest {
  return { ...scope, view_id: viewId };
}
