// Ship Hub — TanStack Query layer for the GitHub PR Kanban + deploy strip.
// Types live in `@multica/core/types/ship` and are re-exported from the
// types barrel; this module only exposes query options + hooks so callers
// don't accidentally import zod or query-client internals.
export {
  shipKeys,
  shipProjectsOptions,
  projectPullRequestsOptions,
  deployEnvironmentsOptions,
  recentDeploysOptions,
  useShipProjects,
  useProjectPullRequests,
  useSyncProject,
  useDeployEnvironments,
  useRecentDeploys,
  useUpsertDeployEnvironment,
  useLogDeploy,
  // Phase 3 chip mutations + recent-actions query
  useMergePullRequest,
  useRebasePullRequestOnMain,
  useCommentOnPullRequest,
  useDismissPullRequestReview,
  useDiagnoseCIFailure,
  useSummarizeReviewFeedback,
  useNudgePullRequestAuthor,
  useRunSmokeTests,
  useClosePullRequestAsStale,
  useShipCardActions,
  shipCardActionsOptions,
  // Phase 5
  shipHubSummaryOptions,
  useShipHubSummary,
  useCreateOrGetDeployPreflight,
  useUpdateDeployPreflight,
  usePromoteDeployPreflight,
  shipSnapshotOptions,
  useShipSnapshot,
} from "./queries";
