import { useQueryClient, useMutation } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { gitlabKeys } from "./queries";


/** Default GitLab label that triggers Multica issue creation. */
export const DEFAULT_GITLAB_ISSUE_SYNC_LABEL = "agent";

export interface GitLabSettings {
  enabled: boolean;
  mrSidebar: boolean;
  issueSync: boolean;
  commentSync: boolean;
  /**
   * GitLab label title that creates/syncs Multica issues. Defaults to "agent"
   * so workspaces predating this setting keep historical behavior.
   */
  issueSyncLabel: string;
}

export function deriveGitLabSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GitLabSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.gitlab_enabled !== false;
  const rawLabel = s.gitlab_issue_sync_label;
  const issueSyncLabel =
    typeof rawLabel === "string" && rawLabel.trim() !== ""
      ? rawLabel.trim()
      : DEFAULT_GITLAB_ISSUE_SYNC_LABEL;
  return {
    enabled,
    mrSidebar: enabled && s.gitlab_mr_sidebar_enabled !== false,
    issueSync: enabled && s.gitlab_issue_sync_enabled !== false,
    commentSync: enabled && s.gitlab_comment_sync_enabled !== false,
    issueSyncLabel,
  };
}

export function useLinkGitLabIssue(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ projectPath, glIssueIid }: { projectPath: string; glIssueIid: number }) =>
      api.linkGitLabIssue(issueId, projectPath, glIssueIid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.gitlabIssue(issueId) });
    },
  });
}

export function useUnlinkGitLabIssue(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.unlinkGitLabIssue(issueId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.gitlabIssue(issueId) });
    },
  });
}

export function useDeleteGitLabConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) => api.deleteGitLabConnection(wsId, connectionId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.connections(wsId) });
    },
  });
}

export function useRotateGitLabWebhookSecret(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) =>
      api.rotateGitLabConnectionWebhookSecret(wsId, connectionId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.connections(wsId) });
    },
  });
}
