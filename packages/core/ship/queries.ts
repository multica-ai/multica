import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateDeployEnvironmentRequest,
  LogDeployRequest,
  PullRequestState,
} from "../types";

// Query key factory — workspace-scoped per CLAUDE.md so a workspace switch
// never serves stale Ship data. The actual workspace context is supplied by
// ApiClient's X-Workspace-Slug header (set by [workspaceSlug] layout); the
// wsId in the key is only for cache isolation.
export const shipKeys = {
  all: (wsId: string) => ["ship", wsId] as const,
  projects: (wsId: string) => [...shipKeys.all(wsId), "projects"] as const,
  pullRequests: (wsId: string, projectId: string, state: string) =>
    [...shipKeys.all(wsId), "pull_requests", projectId, state] as const,
  pullRequestsForProject: (wsId: string, projectId: string) =>
    [...shipKeys.all(wsId), "pull_requests", projectId] as const,
  environments: (wsId: string, projectId: string) =>
    [...shipKeys.all(wsId), "envs", projectId] as const,
  deploys: (wsId: string, environmentId: string) =>
    [...shipKeys.all(wsId), "deploys", environmentId] as const,
};

/** List of projects in the workspace that have ≥1 GitHub repo attached.
 * Backed by GET /api/ship/projects — feature-gated server-side, so when the
 * flag is off the call returns 404 and TanStack Query surfaces an error
 * (the page hides the surface entirely in that case). */
export function shipProjectsOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: shipKeys.projects(wsId),
    queryFn: () => api.listShipProjects(),
    enabled,
    // PR + env counts come from the same endpoint as the project list.
    // Refetch on-mount so the badges in the sidebar widget stay reasonably
    // fresh; WS events also invalidate this on actual change.
    staleTime: 30_000,
  });
}

export function useShipProjects(enabled = true) {
  const wsId = useWorkspaceId();
  return useQuery(shipProjectsOptions(wsId, enabled));
}

/** Pull requests for a project, optionally filtered by state. Default is
 * open-only (matches the Kanban's primary view). Pass "all" to retrieve the
 * full history (used by the "Recently Merged" column derivation, which
 * actually wants merged-state PRs from the last 7 days). */
export function projectPullRequestsOptions(
  wsId: string,
  projectId: string,
  state: PullRequestState | "all" = "open",
) {
  return queryOptions({
    queryKey: shipKeys.pullRequests(wsId, projectId, state),
    queryFn: () => api.listProjectPullRequests(projectId, { state }),
    enabled: !!projectId,
    staleTime: 30_000,
  });
}

export function useProjectPullRequests(
  projectId: string,
  state: PullRequestState | "all" = "open",
) {
  const wsId = useWorkspaceId();
  return useQuery(projectPullRequestsOptions(wsId, projectId, state));
}

/** Manual sync trigger — POST /api/projects/:id/pull_requests/sync. Returns
 * the sync result so the UI can confirm what changed. Errors are surfaced
 * raw to the caller (which translates the 401/429/etc into the UI states
 * defined in the Ship Hub spec). */
export function useSyncProject() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (projectId: string) => api.syncProjectPullRequests(projectId),
    onSettled: (_data, _err, projectId) => {
      // Invalidate every state-filtered cache for this project. Sync may
      // have moved a PR from open → merged, so a single state-scoped
      // invalidation isn't enough.
      qc.invalidateQueries({
        queryKey: shipKeys.pullRequestsForProject(wsId, projectId),
      });
      qc.invalidateQueries({ queryKey: shipKeys.projects(wsId) });
    },
  });
}

export function deployEnvironmentsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: shipKeys.environments(wsId, projectId),
    queryFn: () => api.listProjectDeployEnvironments(projectId),
    enabled: !!projectId,
  });
}

export function useDeployEnvironments(projectId: string) {
  const wsId = useWorkspaceId();
  return useQuery(deployEnvironmentsOptions(wsId, projectId));
}

export function recentDeploysOptions(
  wsId: string,
  environmentId: string,
  limit = 20,
) {
  return queryOptions({
    queryKey: [...shipKeys.deploys(wsId, environmentId), limit] as const,
    queryFn: () => api.listDeploys(environmentId, { limit }),
    enabled: !!environmentId,
  });
}

export function useRecentDeploys(environmentId: string, limit = 20) {
  const wsId = useWorkspaceId();
  return useQuery(recentDeploysOptions(wsId, environmentId, limit));
}

/** Create or update (kind-keyed upsert) a deploy environment for a project.
 * Backend convention: POST /api/projects/:id/deploy_environments is an
 * upsert by `kind`, so creating staging twice patches the existing row. */
export function useUpsertDeployEnvironment(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateDeployEnvironmentRequest) =>
      api.upsertProjectDeployEnvironment(projectId, data),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: shipKeys.environments(wsId, projectId),
      });
      qc.invalidateQueries({ queryKey: shipKeys.projects(wsId) });
    },
  });
}

/** Manually log a deploy attempt (Phase 1 doesn't have webhook ingestion;
 * users record what happened via the "Log deploy" dialog). */
export function useLogDeploy(environmentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: LogDeployRequest) => api.logDeploy(environmentId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.deploys(wsId, environmentId) });
      // The environment row carries `current_sha` / `current_deployed_at`
      // — a successful deploy bumps both, so refresh the env list too.
      qc.invalidateQueries({
        queryKey: [...shipKeys.all(wsId), "envs"] as const,
      });
    },
  });
}
