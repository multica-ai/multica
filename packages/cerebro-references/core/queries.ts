import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { referenceKeys } from "./keys";
import type {
  CreateReferenceInput,
  IssueReference,
  UpdateReferenceInput,
} from "./types";

// Wire-level response wrappers — the backend wraps lists in `{references: []}`
// (handler.go). Normalise away the wrapper so consumers always see arrays.

interface ListResponse {
  references?: IssueReference[];
}

async function fetchIssueReferences(issueId: string): Promise<IssueReference[]> {
  const raw = await api.listCerebroIssueReferences<ListResponse>(issueId);
  if (!raw || !Array.isArray(raw.references)) return [];
  return raw.references;
}

export function useIssueReferences(issueId: string | null | undefined) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: referenceKeys.byIssue(wsId, issueId ?? ""),
    queryFn: () => fetchIssueReferences(issueId as string),
    enabled: Boolean(wsId && issueId),
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function useAddReference(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: CreateReferenceInput) =>
      api.createCerebroIssueReference<IssueReference>(issueId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: referenceKeys.byIssue(wsId, issueId) });
    },
  });
}

export function useUpdateReference(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { refId: string; input: UpdateReferenceInput }) =>
      api.updateCerebroReference<IssueReference>(vars.refId, vars.input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: referenceKeys.byIssue(wsId, issueId) });
    },
  });
}

export function useDeleteReference(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (refId: string) => api.deleteCerebroReference(refId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: referenceKeys.byIssue(wsId, issueId) });
    },
  });
}
