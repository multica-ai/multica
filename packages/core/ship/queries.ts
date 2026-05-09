import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateDeployEnvironmentRequest,
  LogDeployRequest,
  PullRequestState,
  PullRequest,
  ListPullRequestsResponse,
  MergePullRequestRequest,
  CommentPullRequestRequest,
  DismissPullRequestReviewRequest,
  NudgePullRequestAuthorRequest,
  RunSmokeTestsRequest,
  ClosePullRequestAsStaleRequest,
  SubmitPullRequestReviewRequest,
  CreatePreflightRequest,
  UpdatePreflightRequest,
  ConfigureDeployAdapterRequest,
  RollbackDeployRequest,
  CreateReleaseRequest,
  UpdateReleaseRequest,
  CancelReleaseRequest,
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
  // Phase 3 — every pull_requests cache across all projects. Used by the
  // ship:card_action WS handler when the payload doesn't carry project_id
  // (the current backend payload only includes pull_request_id), so we
  // invalidate broadly to keep correctness; the workspace-wide prefix keeps
  // it scoped.
  allPullRequests: (wsId: string) =>
    [...shipKeys.all(wsId), "pull_requests"] as const,
  environments: (wsId: string, projectId: string) =>
    [...shipKeys.all(wsId), "envs", projectId] as const,
  deploys: (wsId: string, environmentId: string) =>
    [...shipKeys.all(wsId), "deploys", environmentId] as const,
  // Phase 3 — recent-actions footer cache. Keyed per PR; an action invalidates
  // the per-PR list. WS event `ship:card_action` triggers the same invalidation.
  cardActions: (wsId: string, prId: string) =>
    [...shipKeys.all(wsId), "card_actions", prId] as const,
  // Phase 5 — workspace summary (ambient sidebar) + preflight per-env-sha
  // + time-machine snapshots.
  summary: (wsId: string) => [...shipKeys.all(wsId), "summary"] as const,
  preflight: (wsId: string, envId: string, sha: string) =>
    [...shipKeys.all(wsId), "preflight", envId, sha] as const,
  snapshot: (wsId: string, projectId: string, at: string) =>
    [...shipKeys.all(wsId), "snapshot", projectId, at] as const,
  // Phase 6 — multi-adapter deploy. Adapter list is workspace-scoped so a
  // workspace switch refetches (different adapters may be available
  // server-side in the future based on plan / feature flags).
  adapters: (wsId: string) => [...shipKeys.all(wsId), "adapters"] as const,
  // Phase 7a — Releases. Workspace-prefixed so a switch wipes
  // every release cache without manual invalidation.
  releases: (wsId: string) => [...shipKeys.all(wsId), "releases"] as const,
  releaseDetail: (wsId: string, releaseId: string) =>
    [...shipKeys.releases(wsId), "detail", releaseId] as const,
  workspaceActiveReleases: (wsId: string) =>
    [...shipKeys.releases(wsId), "active"] as const,
  projectReleases: (wsId: string, projectId: string, status: string) =>
    [...shipKeys.releases(wsId), "by_project", projectId, status] as const,
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

// ---------------------------------------------------------------------------
// Phase 3 — chip mutations
//
// One `useMutation` per chip endpoint. Convention:
//   - `mutationFn` calls the matching api method.
//   - `onSettled` invalidates the workspace-wide pull_requests prefix +
//     the per-PR card-actions cache. We invalidate the PREFIX rather than a
//     single project's PR list because the WS payload doesn't carry
//     project_id, and the chip caller only knows the PR id (not the
//     project id) at call time. The over-invalidation is bounded to the
//     active workspace's ship surface.
//
// Optimistic updates only happen for actions whose effect is deterministic
// from the request alone — `merge` flips state to merged, `close_as_stale`
// flips state to closed. Everything else (comment, rebase, nudge, smoke
// tests) leaves the cache untouched and lets the WS event drive the refetch.
// Optimism elsewhere would create a "fake green" frame on a chip whose
// outcome the user can't verify locally.
// ---------------------------------------------------------------------------

// Helper: walk every cached pull_requests list under this workspace and
// patch the matching PR row in place. Used by the merge / close-as-stale
// mutations to give the user instant feedback while the server roundtrip
// completes. We rely on TanStack's queryClient.setQueriesData with a
// prefix matcher — every state-filter slice (open/closed/merged/all) is
// updated in lockstep so the row doesn't pop columns.
function patchPullRequestInCache(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  prId: string,
  patch: Partial<PullRequest>,
): void {
  qc.setQueriesData<ListPullRequestsResponse>(
    { queryKey: shipKeys.allPullRequests(wsId) },
    (old) => {
      if (!old) return old;
      let mutated = false;
      const next = old.pull_requests.map((p) => {
        if (p.id !== prId) return p;
        mutated = true;
        return { ...p, ...patch };
      });
      // Avoid creating a new object reference when nothing changed — keeps
      // re-renders on unaffected slices to a minimum.
      if (!mutated) return old;
      return { ...old, pull_requests: next };
    },
  );
}

function invalidatePullRequestSurface(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  prId: string,
): void {
  qc.invalidateQueries({ queryKey: shipKeys.allPullRequests(wsId) });
  qc.invalidateQueries({ queryKey: shipKeys.cardActions(wsId, prId) });
  // Open-PR badges on the ship project list also need a refresh whenever a
  // chip can change a PR's open count (merge/close/reopen).
  qc.invalidateQueries({ queryKey: shipKeys.projects(wsId) });
}

export function useMergePullRequest(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body?: MergePullRequestRequest) => api.mergePullRequest(prId, body),
    onMutate: async () => {
      // Optimistic flip — the user's intent is clear and a successful merge
      // moves the card to "Recently Merged". Rolled back from onError if the
      // server rejects (typically 422 — branch not mergeable).
      await qc.cancelQueries({ queryKey: shipKeys.allPullRequests(wsId) });
      const snapshot = qc.getQueriesData<ListPullRequestsResponse>({
        queryKey: shipKeys.allPullRequests(wsId),
      });
      const nowIso = new Date().toISOString();
      patchPullRequestInCache(qc, wsId, prId, {
        state: "merged",
        pr_merged_at: nowIso,
      });
      return { snapshot };
    },
    onError: (_err, _vars, ctx) => {
      // Restore each slice we touched. snapshot is an array of [key, data].
      ctx?.snapshot?.forEach(([key, data]) => {
        qc.setQueryData(key, data);
      });
    },
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useRebasePullRequestOnMain(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.rebasePullRequestOnMain(prId),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useCommentOnPullRequest(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: CommentPullRequestRequest) => api.commentOnPullRequest(prId, body),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useDismissPullRequestReview(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: DismissPullRequestReviewRequest) =>
      api.dismissPullRequestReview(prId, body),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useDiagnoseCIFailure(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.diagnoseCIFailure(prId),
    // No optimistic update — this spawns an agent task; the chip surfaces
    // the in_progress status from the response and lets the WS event drive
    // any subsequent refresh.
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useSummarizeReviewFeedback(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.summarizeReviewFeedback(prId),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useNudgePullRequestAuthor(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body?: NudgePullRequestAuthorRequest) =>
      api.nudgePullRequestAuthor(prId, body),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useRunSmokeTests(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: RunSmokeTestsRequest) => api.runSmokeTests(prId, body),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

// Phase 6.5 — submit a PR review. No optimistic update: we don't know
// the resulting review_decision until the server replies (server runs
// the actual review_decision derivation). The WS card_action event
// triggers the same broad refresh as the other chip mutations.
//
// Invalidates the same surface as the other chip mutations so the
// per-PR card and the card-actions footer pick up the new audit row.
// The PR's conversation channel may have a fresh status post too —
// we conservatively invalidate channel message lists by passing them
// through the queryClient.invalidateQueries with the channel id when
// review.html_url is available, but for now the channel sidebar
// already polls / receives WS events so we lean on those.
export function useSubmitPullRequestReview(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: SubmitPullRequestReviewRequest) =>
      api.submitPullRequestReview(prId, body),
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

export function useClosePullRequestAsStale(prId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body?: ClosePullRequestAsStaleRequest) =>
      api.closePullRequestAsStale(prId, body),
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: shipKeys.allPullRequests(wsId) });
      const snapshot = qc.getQueriesData<ListPullRequestsResponse>({
        queryKey: shipKeys.allPullRequests(wsId),
      });
      const nowIso = new Date().toISOString();
      patchPullRequestInCache(qc, wsId, prId, {
        state: "closed",
        pr_closed_at: nowIso,
      });
      return { snapshot };
    },
    onError: (_err, _vars, ctx) => {
      ctx?.snapshot?.forEach(([key, data]) => {
        qc.setQueryData(key, data);
      });
    },
    onSettled: () => invalidatePullRequestSurface(qc, wsId, prId),
  });
}

// Phase 3 — recent-actions audit footer.
//
// The backend has the underlying SQL (ListShipCardActionsForPR) but the HTTP
// handler isn't registered yet. The query is therefore disabled by default
// — flip `enabled` to true once the route lands. parseWithFallback returns
// an empty list on the 404, so a stray `enabled: true` won't crash.
export function shipCardActionsOptions(wsId: string, prId: string, enabled: boolean) {
  return queryOptions({
    queryKey: shipKeys.cardActions(wsId, prId),
    queryFn: () => api.listShipCardActions(prId),
    enabled,
    staleTime: 15_000,
  });
}

export function useShipCardActions(prId: string, enabled = false) {
  const wsId = useWorkspaceId();
  return useQuery(shipCardActionsOptions(wsId, prId, enabled));
}

// ---------------------------------------------------------------------------
// Phase 5 — workspace summary + pre-flight + time-machine.
// ---------------------------------------------------------------------------

/** Workspace-wide Ship Hub summary. Powers the multi-segment ambient
 *  sidebar widget. Refetches every 30s (the same cadence as the rest
 *  of the ship surface) — WS event invalidation tightens it further. */
export function shipHubSummaryOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: shipKeys.summary(wsId),
    queryFn: () => api.getShipHubSummary(),
    enabled,
    staleTime: 30_000,
  });
}

export function useShipHubSummary(enabled = true) {
  const wsId = useWorkspaceId();
  return useQuery(shipHubSummaryOptions(wsId, enabled));
}

/** Pre-flight checklist — get-or-create on mount of the dialog.
 *  Mutation rather than query because the create endpoint is POST and
 *  we want explicit fetch-on-open semantics. */
export function useCreateOrGetDeployPreflight(environmentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: CreatePreflightRequest) =>
      api.createOrGetDeployPreflight(environmentId, body),
    onSuccess: (preflight) => {
      qc.setQueryData(
        shipKeys.preflight(wsId, environmentId, preflight.target_sha),
        preflight,
      );
    },
  });
}

/** PATCH the preflight checklist. Server recomputes the gate on every
 *  read so the response carries the up-to-date `gate_status` /
 *  `gate_blocked_reasons` — we just store the response in the cache. */
export function useUpdateDeployPreflight(preflightId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: UpdatePreflightRequest) =>
      api.updateDeployPreflight(preflightId, body),
    onSuccess: (preflight) => {
      qc.setQueryData(
        shipKeys.preflight(wsId, preflight.environment_id, preflight.target_sha),
        preflight,
      );
      // The summary's "promotion_pending" segment is derived from
      // preflight rows — invalidate so the sidebar re-counts.
      qc.invalidateQueries({ queryKey: shipKeys.summary(wsId) });
    },
  });
}

export function usePromoteDeployPreflight(preflightId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.promoteDeployPreflight(preflightId),
    onSettled: () => {
      // Promote starts a deploy — every Ship Hub query needs to refresh.
      qc.invalidateQueries({ queryKey: shipKeys.all(wsId) });
    },
  });
}

/** GET /api/projects/{id}/ship_snapshot?at=<RFC3339>. Cached by (project,
 *  at) so dragging the slider doesn't re-fetch the same timestamp twice. */
export function shipSnapshotOptions(
  wsId: string,
  projectId: string,
  at: string | null,
) {
  return queryOptions({
    queryKey: shipKeys.snapshot(wsId, projectId, at ?? ""),
    queryFn: () => api.getProjectShipSnapshot(projectId, at!),
    enabled: !!projectId && !!at,
    staleTime: 60_000,
  });
}

export function useShipSnapshot(projectId: string, at: string | null) {
  const wsId = useWorkspaceId();
  return useQuery(shipSnapshotOptions(wsId, projectId, at));
}

// ---------------------------------------------------------------------------
// Phase 6 — multi-adapter deploy.
// ---------------------------------------------------------------------------

/** List the deploy adapters this server has registered. Drives the
 *  env-config dialog dropdown so adding a new adapter is purely a
 *  server-side change. */
export function deployAdaptersOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: shipKeys.adapters(wsId),
    queryFn: () => api.listDeployAdapters(),
    enabled,
    // Adapters list is effectively static within a server build, but
    // we still refetch on workspace switch (the wsId in the key
    // handles that automatically).
    staleTime: 60 * 60 * 1000,
  });
}

export function useDeployAdapters(enabled = true) {
  const wsId = useWorkspaceId();
  return useQuery(deployAdaptersOptions(wsId, enabled));
}

/** Configure the adapter for a deploy environment. Server encrypts both
 *  the config blob and the optional webhook secret with the workspace's
 *  AES-256-GCM key (same primitive as the GitHub PAT store). */
export function useConfigureDeployAdapter(environmentId: string, projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: ConfigureDeployAdapterRequest) =>
      api.configureDeployAdapter(environmentId, data),
    onSettled: () => {
      // Adapter change affects how the env's deploys are interpreted;
      // refresh the env list so adapter_kind on the row updates and
      // the swimlane re-renders the right adapter icon.
      qc.invalidateQueries({
        queryKey: shipKeys.environments(wsId, projectId),
      });
    },
  });
}

/** Force-poll a deploy environment via its adapter. Returns the updated
 *  current_sha when the provider has a newer SHA than what we have
 *  cached, or `changed: false` when the cache is already up-to-date. */
export function usePollDeployEnvironment(environmentId: string, projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.pollDeployEnvironment(environmentId),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: shipKeys.environments(wsId, projectId),
      });
      qc.invalidateQueries({
        queryKey: shipKeys.deploys(wsId, environmentId),
      });
    },
  });
}

/** Rollback an environment to a prior SHA via its adapter. Owner/admin
 *  only on the server; the UI hides the affordance for non-admin
 *  members upstream. */
export function useRollbackDeployEnvironment(environmentId: string, projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: RollbackDeployRequest) =>
      api.rollbackDeployEnvironment(environmentId, data),
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: shipKeys.environments(wsId, projectId),
      });
      qc.invalidateQueries({
        queryKey: shipKeys.deploys(wsId, environmentId),
      });
      qc.invalidateQueries({ queryKey: shipKeys.summary(wsId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Phase 7a — Releases.
//
// A Release groups a set of PRs through merge → staging → production.
// Phase 7a only implements create / read / cancel; phases 7b+ add stage
// transitions and deploy automation.
//
// Cache layout:
//   * shipKeys.workspaceActiveReleases(wsId) — workspace-wide rail.
//   * shipKeys.projectReleases(wsId, projectId, status) — per-project lists.
//   * shipKeys.releaseDetail(wsId, releaseId) — release detail page.
//
// WS events `release:created` / `release:updated` / `release:cancelled`
// invalidate the rail + the affected detail (see use-realtime-sync.ts).
// ---------------------------------------------------------------------------

/** Workspace-wide active releases rail. Renders on the Ship Hub
 *  landing page above the per-project sections. */
export function workspaceActiveReleasesOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: shipKeys.workspaceActiveReleases(wsId),
    queryFn: () => api.listWorkspaceActiveReleases(wsId),
    enabled,
    staleTime: 30_000,
  });
}

export function useActiveReleases(enabled = true) {
  const wsId = useWorkspaceId();
  return useQuery(workspaceActiveReleasesOptions(wsId, enabled));
}

/** Per-project release list (release detail's "siblings" rail in
 *  Phase 7b+; for 7a it backs the project section's release list). */
export function projectReleasesOptions(
  wsId: string,
  projectId: string,
  status: "active" | "all" = "active",
) {
  return queryOptions({
    queryKey: shipKeys.projectReleases(wsId, projectId, status),
    queryFn: () => api.listProjectReleases(projectId, { status }),
    enabled: !!projectId,
    staleTime: 30_000,
  });
}

export function useProjectReleases(
  projectId: string,
  status: "active" | "all" = "active",
) {
  const wsId = useWorkspaceId();
  return useQuery(projectReleasesOptions(wsId, projectId, status));
}

/** Release detail. */
export function releaseDetailOptions(
  wsId: string,
  releaseId: string,
  enabled: boolean,
) {
  return queryOptions({
    queryKey: shipKeys.releaseDetail(wsId, releaseId),
    queryFn: () => api.getRelease(releaseId),
    enabled,
    staleTime: 15_000,
  });
}

export function useReleaseDetail(releaseId: string, enabled = true) {
  const wsId = useWorkspaceId();
  return useQuery(releaseDetailOptions(wsId, releaseId, enabled && !!releaseId));
}

/** Create release. On success, navigates the caller to the new
 *  detail page (the dialog wires this through). */
export function useCreateRelease(projectId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateReleaseRequest) => api.createRelease(projectId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.workspaceActiveReleases(wsId) });
      qc.invalidateQueries({
        queryKey: shipKeys.projectReleases(wsId, projectId, "active"),
      });
      // Per-PR card decoration also needs to refresh so the
      // release badge shows up immediately on every Kanban card.
      qc.invalidateQueries({ queryKey: shipKeys.allPullRequests(wsId) });
    },
  });
}

/** PATCH release metadata. */
export function useUpdateRelease(releaseId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: UpdateReleaseRequest) => api.updateRelease(releaseId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.releaseDetail(wsId, releaseId) });
      qc.invalidateQueries({ queryKey: shipKeys.workspaceActiveReleases(wsId) });
    },
  });
}

/** Add a PR to an assembling release. */
export function useAddPullRequestToRelease(releaseId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { pull_request_id: string }) =>
      api.addPullRequestToRelease(releaseId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.releaseDetail(wsId, releaseId) });
      qc.invalidateQueries({ queryKey: shipKeys.allPullRequests(wsId) });
    },
  });
}

/** Remove a PR from an assembling release. */
export function useRemovePullRequestFromRelease(releaseId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (pullRequestId: string) =>
      api.removePullRequestFromRelease(releaseId, pullRequestId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.releaseDetail(wsId, releaseId) });
      qc.invalidateQueries({ queryKey: shipKeys.allPullRequests(wsId) });
    },
  });
}

/** Cancel an assembling release. */
export function useCancelRelease(releaseId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data?: CancelReleaseRequest) => api.cancelRelease(releaseId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: shipKeys.releaseDetail(wsId, releaseId) });
      qc.invalidateQueries({ queryKey: shipKeys.workspaceActiveReleases(wsId) });
      qc.invalidateQueries({ queryKey: shipKeys.allPullRequests(wsId) });
    },
  });
}
