import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { integrationKeys } from "./queries";
import type { IntegrationProvider } from "../types";

export function useUpsertWorkspaceIntegration() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { provider: IntegrationProvider; instance_url: string }) =>
      api.upsertWorkspaceIntegration(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: integrationKeys.workspace(wsId) });
    },
  });
}

export function useDeleteWorkspaceIntegration() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (provider: IntegrationProvider) => api.deleteWorkspaceIntegration(provider),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: integrationKeys.workspace(wsId) });
    },
  });
}

export function useUpsertMyCredential() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ provider, apiKey }: { provider: IntegrationProvider; apiKey: string }) =>
      api.upsertMyCredential(provider, apiKey),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: integrationKeys.credential(wsId, vars.provider) });
    },
  });
}

export function useDeleteMyCredential() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (provider: IntegrationProvider) => api.deleteMyCredential(provider),
    onSettled: (_data, _err, provider) => {
      qc.invalidateQueries({ queryKey: integrationKeys.credential(wsId, provider) });
    },
  });
}

export function useUpsertProjectIntegrationLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      projectId,
      ...data
    }: {
      projectId: string;
      provider: IntegrationProvider;
      external_project_id: string;
      external_project_name?: string | null;
    }) => api.upsertProjectIntegrationLink(projectId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: integrationKeys.projectLinks(wsId, vars.projectId) });
    },
  });
}

export function useDeleteProjectIntegrationLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ projectId, provider }: { projectId: string; provider: IntegrationProvider }) =>
      api.deleteProjectIntegrationLink(projectId, provider),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: integrationKeys.projectLinks(wsId, vars.projectId) });
    },
  });
}

export function useUpsertIssueIntegrationLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      issueId,
      ...data
    }: {
      issueId: string;
      provider: IntegrationProvider;
      external_issue_id: string;
      external_issue_url?: string | null;
      external_issue_title?: string | null;
    }) => api.upsertIssueIntegrationLink(issueId, data),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: integrationKeys.issueLinks(wsId, vars.issueId) });
    },
  });
}

export function useDeleteIssueIntegrationLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, provider }: { issueId: string; provider: IntegrationProvider }) =>
      api.deleteIssueIntegrationLink(issueId, provider),
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: integrationKeys.issueLinks(wsId, vars.issueId) });
    },
  });
}
