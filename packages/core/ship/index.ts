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
  // Phase 6.5
  useSubmitPullRequestReview,
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
  // Phase 6 — multi-adapter deploy
  deployAdaptersOptions,
  useDeployAdapters,
  useConfigureDeployAdapter,
  usePollDeployEnvironment,
  useRollbackDeployEnvironment,
  // Phase 7a — Releases
  workspaceActiveReleasesOptions,
  useActiveReleases,
  projectReleasesOptions,
  useProjectReleases,
  releaseDetailOptions,
  useReleaseDetail,
  useCreateRelease,
  useUpdateRelease,
  useAddPullRequestToRelease,
  useRemovePullRequestFromRelease,
  useCancelRelease,
  // Phase 7b — Merge train
  useStartMergeTrain,
  useResumeMergeTrain,
  useAbortMergeTrain,
  // Phase 7c — Staging deploy linkage + smoke + verify gate
  useRunSmokeTestsForRelease,
  useMarkSmokeManualPass,
  useMarkReleaseVerified,
  useUnverifyRelease,
  useMarkReleaseStagingDeployed,
  // Phase 7d — Production promotion + rollback + health rollup
  usePromoteRelease,
  useMarkReleaseProductionDeployed,
  useRollbackRelease,
  useMarkReleaseDone,
  useReleaseHealth,
  releaseHealthOptions,
  // PR detail drawer — bundled per-PR query.
  pullRequestDetailsOptions,
  usePullRequestDetails,
} from "./queries";

// Phase 7a — multi-select store. Lives next to the queries because
// the selection drives a release-creation flow and the dialog wants
// both the selected PR ids and the release mutations in one place.
export {
  useShipSelection,
  useShipSelectionCount,
} from "./selection";

// PR detail drawer — open/close state. Shared store so the Kanban
// and the release page can both dispatch `open(prId)`.
export {
  useShipPrDetailStore,
  useShipPrDetailOpenId,
} from "./pr-detail-store";

// Persisted per-workspace project-collapse preferences for the Ship
// Hub landing page. See collapsed-projects-store.ts.
export { useCollapsedProjects } from "./collapsed-projects-store";
