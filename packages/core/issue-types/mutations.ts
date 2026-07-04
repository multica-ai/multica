import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueTypeKeys } from "./queries";
import type { CreateIssueTypeRequest, UpdateIssueTypeRequest } from "../types";

export function useCreateIssueType() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, ...data }: { workspaceId: string } & CreateIssueTypeRequest) =>
      api.createIssueType(workspaceId, data),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: issueTypeKeys.all(variables.workspaceId) });
    },
  });
}

export function useUpdateIssueType() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, issueTypeId, ...data }: { workspaceId: string; issueTypeId: string } & UpdateIssueTypeRequest) =>
      api.updateIssueType(workspaceId, issueTypeId, data),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: issueTypeKeys.all(variables.workspaceId) });
    },
  });
}

export function useDeleteIssueType() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId, issueTypeId }: { workspaceId: string; issueTypeId: string }) =>
      api.deleteIssueType(workspaceId, issueTypeId),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: issueTypeKeys.all(variables.workspaceId) });
    },
  });
}

export function useSeedDefaultIssueTypes() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ workspaceId }: { workspaceId: string }) =>
      api.seedDefaultIssueTypes(workspaceId),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: issueTypeKeys.all(variables.workspaceId) });
    },
  });
}
