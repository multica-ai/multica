import { useQueryClient, useMutation } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { gitlabKeys } from "./queries";


export interface GitLabSettings {
  enabled: boolean;
  mrSidebar: boolean;
  issueSync: boolean;
  commentSync: boolean;
}

export function deriveGitLabSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GitLabSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.gitlab_enabled !== false;
  return {
    enabled,
    mrSidebar: enabled && s.gitlab_mr_sidebar_enabled !== false,
    issueSync: enabled && s.gitlab_issue_sync_enabled !== false,
    commentSync: enabled && s.gitlab_comment_sync_enabled !== false,
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
