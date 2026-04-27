import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IntegrationProvider } from "../types";

export const integrationKeys = {
  workspace: (wsId: string) => ["integrations", wsId, "workspace"] as const,
  credential: (wsId: string, provider: IntegrationProvider) =>
    ["integrations", wsId, "credential", provider] as const,
  projectLinks: (wsId: string, projectId: string) =>
    ["integrations", wsId, "project", projectId] as const,
  issueLinks: (wsId: string, issueId: string) =>
    ["integrations", wsId, "issue", issueId] as const,
  redmineProjects: (wsId: string) =>
    ["integrations", wsId, "redmine", "projects"] as const,
  redmineIssues: (wsId: string, projectId: number) =>
    ["integrations", wsId, "redmine", "issues", projectId] as const,
};

export function workspaceIntegrationsOptions(wsId: string) {
  return queryOptions({
    queryKey: integrationKeys.workspace(wsId),
    queryFn: () => api.listWorkspaceIntegrations(),
    enabled: !!wsId,
  });
}

export function myCredentialOptions(wsId: string, provider: IntegrationProvider) {
  return queryOptions({
    queryKey: integrationKeys.credential(wsId, provider),
    queryFn: () => api.getMyCredential(provider),
    enabled: !!wsId,
  });
}

export function projectIntegrationLinksOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: integrationKeys.projectLinks(wsId, projectId),
    queryFn: () => api.listProjectIntegrationLinks(projectId),
    enabled: !!wsId && !!projectId,
  });
}

export function issueIntegrationLinksOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: integrationKeys.issueLinks(wsId, issueId),
    queryFn: () => api.listIssueIntegrationLinks(issueId),
    enabled: !!wsId && !!issueId,
  });
}

export function redmineProjectsOptions(wsId: string) {
  return queryOptions({
    queryKey: integrationKeys.redmineProjects(wsId),
    queryFn: () => api.listRedmineProjects(),
    enabled: !!wsId,
  });
}

export function redmineIssuesOptions(wsId: string, projectId: number) {
  return queryOptions({
    queryKey: integrationKeys.redmineIssues(wsId, projectId),
    queryFn: () => api.listRedmineIssues(projectId),
    enabled: !!wsId && !!projectId,
  });
}
