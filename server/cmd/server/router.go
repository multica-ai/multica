package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var defaultOrigins = []string{
	"http://localhost:3000", // Next.js dev
	"http://localhost:5173", // electron-vite dev
	"http://localhost:5174", // electron-vite dev (fallback port)
}

func allowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return defaultOrigins
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return defaultOrigins
	}
	return origins
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
// rdb is optional: when non-nil the runtime local-skill request stores are
// swapped for Redis-backed implementations so multiple API nodes share the
// same pending queue (required for multi-node prod). This should be a request
// path Redis client, not the realtime relay's blocking read client. A nil rdb
// keeps the default in-memory stores which are fine for single-node dev and
// tests.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client) chi.Router {
	return NewRouterWithOptions(pool, hub, bus, analyticsClient, rdb, RouterOptions{})
}

type RouterOptions struct {
	HTTPMetrics  *obsmetrics.HTTPMetrics
	DaemonHub    *daemonws.Hub
	DaemonWakeup service.TaskWakeupNotifier
	// HeartbeatScheduler, when non-nil, replaces the default synchronous
	// passthrough scheduler on the constructed Handler. main.go injects a
	// BatchedHeartbeatScheduler here so the caller can also drive Run/Stop;
	// tests leave this nil and get the legacy synchronous behavior.
	HeartbeatScheduler handler.HeartbeatScheduler
	// ServiceCtx is a long-lived context handlers can hand to
	// background goroutines (Phase 7b's merge train). Tests leave
	// this nil and the handler falls back to context.Background.
	ServiceCtx context.Context
}

func NewRouterWithOptions(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client, opts RouterOptions) chi.Router {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()
	daemonHub := opts.DaemonHub
	if daemonHub == nil {
		daemonHub = daemonws.NewHub()
	}

	// Initialize storage with S3 as primary, fallback to local
	var store storage.Storage
	s3 := storage.NewS3StorageFromEnv()
	if s3 != nil {
		store = s3
	} else {
		local := storage.NewLocalStorageFromEnv()
		if local != nil {
			store = local
		}
	}

	cfSigner := auth.NewCloudFrontSignerFromEnv()

	signupConfig := handler.Config{
		AllowSignup:                   os.Getenv("ALLOW_SIGNUP") != "false",
		AllowedEmails:                 splitAndTrim(os.Getenv("ALLOWED_EMAILS")),
		AllowedEmailDomains:           splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS")),
		UseDailyRollupForRuntimeUsage: os.Getenv("USAGE_DAILY_ROLLUP_ENABLED") == "true",
	}
	h := handler.New(queries, pool, hub, bus, emailSvc, store, cfSigner, analyticsClient, signupConfig, daemonHub)
	if opts.DaemonWakeup != nil {
		h.TaskService.Wakeup = opts.DaemonWakeup
	}
	if rdb != nil {
		h.UpdateStore = handler.NewRedisUpdateStore(rdb)
		h.ModelListStore = handler.NewRedisModelListStore(rdb)
		h.LocalSkillListStore = handler.NewRedisLocalSkillListStore(rdb)
		h.LocalSkillImportStore = handler.NewRedisLocalSkillImportStore(rdb)
		h.LivenessStore = handler.NewRedisLivenessStore(rdb)
	}
	if opts.HeartbeatScheduler != nil {
		h.HeartbeatScheduler = opts.HeartbeatScheduler
	}
	if opts.ServiceCtx != nil {
		h.ServiceCtx = opts.ServiceCtx
	}
	// Auth caches: PAT cache is shared between the regular Auth middleware,
	// the DaemonAuth fallback (mul_) path, and the revoke handler
	// (invalidate). DaemonTokenCache backs the DaemonAuth mdt_ path. Both
	// constructors return nil when rdb is nil — every consumer handles that
	// as "no cache, always hit DB".
	patCache := auth.NewPATCache(rdb)
	daemonTokenCache := auth.NewDaemonTokenCache(rdb)
	h.PATCache = patCache
	h.DaemonTokenCache = daemonTokenCache

	// Empty-claim cache: lets the daemon poll path skip a Postgres
	// scan when a recent check confirmed the runtime had no queued
	// task. Returns nil when rdb is nil — TaskService treats that
	// as "no cache, always hit DB" (existing behavior).
	h.TaskService.EmptyClaim = service.NewEmptyClaimCache(rdb)

	// Wire WS heartbeat after stores are finalized so the WS path uses the
	// same (possibly Redis-backed) stores as the HTTP path.
	daemonHub.SetHeartbeatHandler(h.HandleDaemonWSHeartbeat)
	health := newServerHealth(pool)

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(middleware.ClientMetadata)
	r.Use(middleware.RequestLogger)
	if opts.HTTPMetrics != nil {
		r.Use(opts.HTTPMetrics.Middleware)
	}
	r.Use(chimw.Recoverer)
	r.Use(middleware.ContentSecurityPolicy)
	origins := allowedOrigins()

	// Share allowed origins with WebSocket origin checker.
	realtime.SetAllowedOrigins(origins)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Workspace-ID", "X-Workspace-Slug", "X-Request-ID", "X-Agent-ID", "X-Task-ID", "X-CSRF-Token", "X-Client-Platform", "X-Client-Version", "X-Client-OS"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health / readiness checks
	r.Get("/health", health.liveHandler)
	r.Get("/readyz", health.readyHandler)
	r.Get("/healthz", health.readyHandler)

	// Realtime subsystem metrics — connection counts, slow-client evictions,
	// and per-event-type send QPS counters. Exposed as JSON so it can be
	// scraped by ops or surfaced in the admin UI without adding a Prometheus
	// dependency. See MUL-1138 (Phase 0).
	//
	// Access is restricted (MUL-1342): when REALTIME_METRICS_TOKEN is set,
	// callers must present it via Authorization: Bearer <token>. When the
	// env var is unset the handler only serves loopback callers so local
	// dev keeps working without exposing the metrics on a public listener.
	r.Get("/health/realtime", realtimeMetricsHandler(os.Getenv("REALTIME_METRICS_TOKEN")))

	// WebSocket
	mc := &membershipChecker{queries: queries}
	pr := &patResolver{queries: queries, cache: patCache}
	slugResolver := realtime.SlugResolver(func(ctx context.Context, slug string) (string, error) {
		ws, err := queries.GetWorkspaceBySlug(ctx, slug)
		if err != nil {
			return "", err
		}
		return util.UUIDToString(ws.ID), nil
	})
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, mc, pr, slugResolver, w, r)
	})

	// Local file serving (when using local storage)
	if local, ok := store.(*storage.LocalStorage); ok {
		r.Get("/uploads/*", func(w http.ResponseWriter, r *http.Request) {
			file := strings.TrimPrefix(r.URL.Path, "/uploads/")
			local.ServeFile(w, r, file)
		})
	}

	// Auth (public)
	r.Post("/auth/send-code", h.SendCode)
	r.Post("/auth/verify-code", h.VerifyCode)
	r.Post("/auth/google", h.GoogleLogin)
	r.Post("/auth/logout", h.Logout)

	// Public API
	r.Get("/api/config", h.GetConfig)

	// Ship Hub Phase 2: GitHub webhook receiver. UNAUTHENTICATED — the
	// HMAC signature is the only auth, and signature verification scans
	// every workspace's stored secret. Mounted OUTSIDE the workspace
	// middleware because GitHub never sends an X-Workspace-ID and we
	// don't want the requests bouncing off auth before the signature is
	// checked.
	r.Post("/api/integrations/github/webhook", h.HandleGitHubWebhook)

	// Ship Hub Phase 6: multi-adapter deploy webhook receiver. Same
	// unauthenticated stance as the GitHub one (signature verifies who
	// the sender is), but routed by adapter kind so adding a 6th
	// adapter doesn't require a new endpoint.
	r.Post("/api/integrations/deploy/{adapter}/webhook", h.HandleDeployAdapterWebhook)

	// Daemon API routes (require daemon token or valid user token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries, patCache, daemonTokenCache))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/ws", h.DaemonWebSocket)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)
		r.Post("/runtimes/{runtimeId}/models/{requestId}/result", h.ReportModelListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/{requestId}/result", h.ReportLocalSkillListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/import/{requestId}/result", h.ReportLocalSkillImportResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/usage", h.ReportTaskUsage)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)

		r.Get("/issues/{issueId}/gc-check", h.GetIssueGCCheck)
		r.Get("/chat-sessions/{sessionId}/gc-check", h.GetChatSessionGCCheck)
		r.Get("/autopilot-runs/{runId}/gc-check", h.GetAutopilotRunGCCheck)
		r.Get("/tasks/{taskId}/gc-check", h.GetTaskGCCheck)

		r.Post("/runtimes/{runtimeId}/recover-orphans", h.RecoverOrphanedTasks)
		r.Post("/tasks/{taskId}/session", h.PinTaskSession)
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Patch("/api/me/onboarding", h.PatchOnboarding)
		r.Post("/api/me/onboarding/complete", h.CompleteOnboarding)
		r.Post("/api/me/onboarding/cloud-waitlist", h.JoinCloudWaitlist)
		r.Post("/api/me/starter-content/import", h.ImportStarterContent)
		r.Post("/api/me/starter-content/dismiss", h.DismissStarterContent)
		r.Post("/api/cli-token", h.IssueCliToken)
		r.Post("/api/upload-file", h.UploadFile)
		r.Post("/api/feedback", h.CreateFeedback)

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				// Member-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					r.Get("/members", h.ListMembersWithUser)
					r.Post("/leave", h.LeaveWorkspace)
					r.Get("/invitations", h.ListWorkspaceInvitations)
				})
				// Admin-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/members", h.CreateInvitation)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
					})
					r.Delete("/invitations/{invitationId}", h.RevokeInvitation)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
				// Ship Hub webhook secret rotation (owner-only). Returns
				// the plaintext exactly once; subsequent reads see only
				// the *_set boolean on the workspace response.
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).
					Post("/ship_hub/regenerate_webhook_secret", h.RegenerateShipHubWebhookSecret)
			})
		})

		// User-scoped invitation routes (no workspace context required)
		r.Get("/api/invitations", h.ListMyInvitations)
		r.Get("/api/invitations/{id}", h.GetMyInvitation)
		r.Post("/api/invitations/{id}/accept", h.AcceptInvitation)
		r.Post("/api/invitations/{id}/decline", h.DeclineInvitation)

		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// --- Workspace-scoped routes (all require workspace membership) ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			// Assignee frequency
			r.Get("/api/assignee-frequency", h.GetAssigneeFrequency)

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/search", h.SearchIssues)
				r.Get("/child-progress", h.ChildIssueProgress)
				r.Get("/", h.ListIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/quick-create", h.QuickCreateIssue)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssue)
					// NOTE: UpdateIssue is a partial-update handler — every field
					// is a *T pointer and only fields present in the body are
					// applied. PUT predates PATCH here for historical reasons
					// (the CLI uses PutJSON); we register PATCH on the same
					// handler so the MCP client (which only has a `patch()`
					// method, no `put()`) can call it without hitting 405.
					r.Put("/", h.UpdateIssue)
					r.Patch("/", h.UpdateIssue)
					r.Delete("/", h.DeleteIssue)
					r.Post("/comments", h.CreateComment)
					r.Get("/comments", h.ListComments)
					r.Get("/timeline", h.ListTimeline)
					r.Get("/subscribers", h.ListIssueSubscribers)
					r.Post("/subscribe", h.SubscribeToIssue)
					r.Post("/unsubscribe", h.UnsubscribeFromIssue)
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Post("/rerun", h.RerunIssue)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Get("/usage", h.GetIssueUsage)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
					r.Get("/children", h.ListChildIssues)
					r.Get("/labels", h.ListLabelsForIssue)
					r.Post("/labels", h.AttachLabel)
					r.Delete("/labels/{labelId}", h.DetachLabel)
				})
			})

			// Task messages (user-facing, not daemon auth)
			r.Get("/api/tasks/{taskId}/messages", h.ListTaskMessagesByUser)

			// Labels
			r.Route("/api/labels", func(r chi.Router) {
				r.Get("/", h.ListLabels)
				r.Post("/", h.CreateLabel)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetLabel)
					r.Put("/", h.UpdateLabel)
					r.Delete("/", h.DeleteLabel)
				})
			})

			// Projects
			r.Route("/api/projects", func(r chi.Router) {
				r.Get("/search", h.SearchProjects)
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					r.Post("/archive", h.ArchiveProject)
					r.Post("/restore", h.RestoreProject)
					r.Get("/resources", h.ListProjectResources)
					r.Post("/resources", h.CreateProjectResource)
					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
				})
			})

			// Ship Hub (Phase 1, read-only). Endpoints respond 404 when
			// workspace.ship_hub_enabled is FALSE — gate lives inside the
			// handlers so the surface is invisible to non-opted-in workspaces.
			r.Route("/api/ship", func(r chi.Router) {
				r.Get("/projects", h.ListShipProjects)
			})
			// Project-scoped Ship Hub endpoints share the project router with
			// the existing /api/projects/{id} routes below; we register them
			// outside that block so the handlers can run their own feature gate
			// (and so a workspace without ship_hub_enabled doesn't surface
			// these on the regular project page).
			r.Get("/api/projects/{id}/pull_requests", h.ListProjectPullRequests)
			r.Post("/api/projects/{id}/pull_requests/sync", h.SyncProjectPullRequests)
			r.Get("/api/projects/{id}/deploy_environments", h.ListProjectDeployEnvironments)
			r.Post("/api/projects/{id}/deploy_environments", h.CreateProjectDeployEnvironment)
			r.Patch("/api/deploy_environments/{id}", h.UpdateDeployEnvironment)
			r.Get("/api/deploy_environments/{id}/deploys", h.ListDeploys)
			r.Post("/api/deploy_environments/{id}/deploys", h.LogDeploy)

			// Ship Hub Phase 3 — card action chips. Each endpoint runs
			// the workspace-member middleware (this block); per-action
			// destructive checks (owner/admin) live inside the handler
			// so the gate stays close to the action definition.
			r.Post("/api/pull_requests/{id}/merge", h.MergePullRequest)
			r.Post("/api/pull_requests/{id}/rebase_on_main", h.RebasePullRequestOnMain)
			r.Post("/api/pull_requests/{id}/comment", h.CommentOnPullRequest)
			r.Post("/api/pull_requests/{id}/dismiss_review", h.DismissPullRequestReview)
			r.Post("/api/pull_requests/{id}/diagnose_ci_failure", h.DiagnoseCIFailure)
			r.Post("/api/pull_requests/{id}/summarize_review_feedback", h.SummarizeReviewFeedback)
			r.Post("/api/pull_requests/{id}/nudge_author", h.NudgeAuthor)
			r.Post("/api/pull_requests/{id}/run_smoke_tests", h.RunSmokeTests)
			r.Post("/api/pull_requests/{id}/close_as_stale", h.ClosePullRequestAsStale)
			// Phase 6.5 — submit a PR review (Approve / Request changes /
			// Comment) without leaving Multica. Workspace-member auth (any
			// member can review); the destructive gate doesn't apply.
			r.Post("/api/pull_requests/{id}/review", h.SubmitPullRequestReview)

			// Ship Hub Phase 4 — issue↔PR linkage + agent talk-back +
			// PR conversation channels + stack visualization. None of
			// these endpoints touch GitHub directly so they don't
			// require the workspace token; the ship_hub_enabled gate
			// still applies (handled per-handler).
			r.Patch("/api/pull_requests/{id}", h.UpdatePullRequest)
			r.Get("/api/pull_requests/{id}/linked_issues", h.GetLinkedIssues)
			r.Post("/api/pull_requests/{id}/talk_to_agent", h.TalkToAgent)
			r.Post("/api/pull_requests/{id}/conversation_channel", h.GetOrCreatePRConversationChannel)
			r.Get("/api/issues/{id}/pull_requests", h.ListIssuePullRequests)
			r.Get("/api/projects/{id}/pull_request_stacks", h.ListProjectPRStacks)
			// PR detail drawer — bundled response for the in-app
			// drawer. Single round-trip so the Sheet renders without
			// an N+1 fetch on open. Same workspace-member middleware
			// as the Phase 4 endpoints above.
			r.Get("/api/pull_requests/{id}/details", h.GetPullRequestDetails)

			// Ship Hub Phase 5 — pre-flight gate + workspace summary +
			// time-machine snapshot. Same ship_hub_enabled gate; no
			// token required. The summary endpoint sits under
			// /api/workspaces/{id} so it shares the existing workspace
			// scoping middleware applied above.
			r.Post("/api/deploy_environments/{id}/preflight", h.CreateOrGetDeployPreflight)
			r.Patch("/api/deploy_preflight/{id}", h.UpdateDeployPreflight)
			r.Post("/api/deploy_preflight/{id}/promote", h.PromoteDeployPreflight)
			r.Get("/api/ship_hub/summary", h.GetShipHubSummary)
			r.Get("/api/projects/{id}/ship_snapshot", h.GetProjectShipSnapshot)

			// Ship Hub Phase 6 — multi-adapter deploy. List registered
			// adapters; configure / poll an env; rollback an env. The
			// adapter-config write and the rollback dispatch are
			// owner/admin-gated downstream; poll is members-OK because
			// reading current SHA can't damage anything.
			r.Get("/api/deploy/adapters", h.ListDeployAdapters)
			r.Put("/api/deploy_environments/{id}/adapter", h.ConfigureDeployAdapter)
			r.Post("/api/deploy_environments/{id}/poll_now", h.PollDeployEnvironment)
			r.Post("/api/deploy_environments/{id}/rollback", h.RollbackDeployEnvironment)

			// Ship Hub Phase 7a — Releases. Same ship_hub_enabled gate; no
			// GitHub token required for create/list/cancel because the
			// service layer just reads the local pull_request rows. The
			// merge-train (Phase 7b), staging promote (7c), and prod
			// promote (7d) endpoints land later under similar paths.
			r.Post("/api/projects/{id}/releases", h.CreateRelease)
			r.Get("/api/projects/{id}/releases", h.ListProjectReleases)
			r.Get("/api/workspaces/{id}/releases/active", h.ListWorkspaceActiveReleases)
			r.Get("/api/releases/{id}", h.GetRelease)
			r.Patch("/api/releases/{id}", h.UpdateRelease)
			r.Post("/api/releases/{id}/pull_requests", h.AddPullRequestToRelease)
			r.Delete("/api/releases/{id}/pull_requests/{pr_id}", h.RemovePullRequestFromRelease)
			r.Post("/api/releases/{id}/cancel", h.CancelRelease)
			r.Post("/api/releases/{id}/channel", h.OpenReleaseChannel)

			// Phase 7b — Merge train orchestration.
			r.Post("/api/releases/{id}/start_merge", h.StartMergeRelease)
			r.Post("/api/releases/{id}/resume_merge", h.ResumeMergeRelease)
			r.Post("/api/releases/{id}/abort_merge", h.AbortMergeRelease)
			r.Get("/api/releases/{id}/merge_state", h.GetReleaseMergeState)

			// Phase 7c — Staging deploy linkage + smoke + verify gate.
			r.Post("/api/releases/{id}/run_smoke_tests", h.RunSmokeTestsForRelease)
			r.Post("/api/releases/{id}/mark_smoke_pass", h.MarkSmokePass)
			r.Post("/api/releases/{id}/mark_verified", h.MarkReleaseVerified)
			r.Post("/api/releases/{id}/unverify", h.UnverifyRelease)
			// Phase 7c polish — manual escape hatch for repos whose CI
			// doesn't fire GitHub deployment_status events. Synthesizes
			// a deploy row + runs the same linkage flow.
			r.Post("/api/releases/{id}/mark_staging_deployed", h.MarkReleaseStagingDeployed)

			// Phase 7d — Production promotion + post-deploy + rollback.
			r.Post("/api/releases/{id}/promote", h.PromoteRelease)
			r.Post("/api/releases/{id}/mark_production_deployed", h.MarkReleaseProductionDeployed)
			r.Post("/api/releases/{id}/rollback", h.RollbackRelease)
			r.Post("/api/releases/{id}/mark_done", h.MarkReleaseDone)
			r.Get("/api/releases/{id}/health", h.GetReleaseHealth)

			// Channels (multi-participant chat + DMs).
			// Endpoints respond 404 when workspace.channels_enabled is FALSE
			// — the gate lives inside each handler so the surface is invisible
			// to anyone in a workspace that hasn't opted in.
			r.Route("/api/channels", func(r chi.Router) {
				// Phase 5c — full-text search. Mounted ahead of /{channelId}
				// so chi doesn't try to interpret "search" as a UUID.
				r.Get("/search", h.SearchChannelMessages)
				r.Get("/", h.ListChannels)
				r.Post("/", h.CreateChannel)
				r.Route("/{channelId}", func(r chi.Router) {
					r.Get("/", h.GetChannel)
					r.Patch("/", h.UpdateChannel)
					r.Delete("/", h.ArchiveChannel)
					r.Post("/read", h.MarkChannelRead)
					r.Get("/members", h.ListChannelMembers)
					r.Post("/members", h.AddChannelMember)
					r.Delete("/members/{memberType}/{memberId}", h.RemoveChannelMember)
					r.Get("/messages", h.ListChannelMessages)
					r.Post("/messages", h.CreateChannelMessage)
					// Phase 4 — per-message endpoints (threads + reactions).
					// Nested so the channel-access gate covers them via
					// requireChannelAccess inside each handler. The
					// {messageId} URL param is shared across all three.
					r.Route("/messages/{messageId}", func(r chi.Router) {
						// Phase 5 — author / admin edits + soft delete.
						r.Patch("/", h.UpdateChannelMessage)
						r.Delete("/", h.DeleteChannelMessage)
						r.Get("/thread", h.ListChannelMessageThread)
						// "Convert thread → issue" dispatch. The picker
						// sits in the ThreadPanel; POST creates a queued
						// thread-issue task that runs the agent in full
						// issue-task mode against the embedded thread.
						r.Post("/dispatch-issue-task", h.DispatchThreadIssueTask)
						r.Post("/reactions", h.AddChannelReaction)
						r.Delete("/reactions", h.RemoveChannelReaction)
					})
				})
			})
			r.Post("/api/dms", h.CreateOrFetchDM)

			// Memory artifacts — workspace-scoped knowledge primitives
			// (wiki pages, agent notes, runbooks, decision logs). Single
			// polymorphic table; `kind` query param filters per surface.
			r.Route("/api/memory", func(r chi.Router) {
				r.Get("/", h.ListMemoryArtifacts)
				r.Post("/", h.CreateMemoryArtifact)
				r.Get("/search", h.SearchMemoryArtifacts)
				r.Get("/by-anchor/{anchorType}/{anchorId}", h.ListMemoryArtifactsByAnchor)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetMemoryArtifact)
					r.Put("/", h.UpdateMemoryArtifact)
					r.Delete("/", h.DeleteMemoryArtifact)
					r.Post("/archive", h.ArchiveMemoryArtifact)
					r.Post("/restore", h.RestoreMemoryArtifact)
					// History endpoints — list revisions, get a specific
					// revision in full, restore (which is itself a new
					// edit, snapshotting the current state first).
					r.Get("/history", h.ListMemoryArtifactRevisions)
					r.Get("/history/{revision}", h.GetMemoryArtifactRevision)
					r.Post("/restore-revision/{revision}", h.RestoreMemoryArtifactRevision)
				})
			})

			// Autopilots
			r.Route("/api/autopilots", func(r chi.Router) {
				r.Get("/", h.ListAutopilots)
				r.Post("/", h.CreateAutopilot)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAutopilot)
					r.Patch("/", h.UpdateAutopilot)
					r.Delete("/", h.DeleteAutopilot)
					r.Post("/trigger", h.TriggerAutopilot)
					r.Get("/runs", h.ListAutopilotRuns)
					r.Post("/triggers", h.CreateAutopilotTrigger)
					r.Route("/triggers/{triggerId}", func(r chi.Router) {
						r.Patch("/", h.UpdateAutopilotTrigger)
						r.Delete("/", h.DeleteAutopilotTrigger)
					})
				})
			})

			// Pins
			r.Route("/api/pins", func(r chi.Router) {
				r.Get("/", h.ListPins)
				r.Post("/", h.CreatePin)
				r.Put("/reorder", h.ReorderPins)
				r.Delete("/{itemType}/{itemId}", h.DeletePin)
			})

			// Attachments
			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			// Comments
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			// Agents
			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAgent)
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Post("/cancel-tasks", h.CancelAgentTasks)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)
					r.Get("/tags", h.ListAgentTagsForAgent)
					r.Post("/tags", h.AddTagToAgent)
					r.Delete("/tags/{tagId}", h.RemoveTagFromAgent)
				})
			})

			// Agent tags (workspace-level CRUD)
			r.Route("/api/agent-tags", func(r chi.Router) {
				r.Get("/", h.ListAgentTags)
				r.Post("/", h.CreateAgentTag)
				r.Delete("/{id}", h.DeleteAgentTag)
			})

			// Skills
			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.Post("/", h.CreateSkill)
				r.Post("/import", h.ImportSkill)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
				})
			})

			// Usage
			r.Route("/api/usage", func(r chi.Router) {
				r.Get("/daily", h.GetWorkspaceUsageByDay)
				r.Get("/summary", h.GetWorkspaceUsageSummary)
			})

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Route("/{runtimeId}", func(r chi.Router) {
					r.Get("/usage", h.GetRuntimeUsage)
					r.Get("/usage/by-agent", h.GetRuntimeUsageByAgent)
					r.Get("/usage/by-hour", h.GetRuntimeUsageByHour)
					r.Get("/activity", h.GetRuntimeTaskActivity)
					r.Post("/update", h.InitiateUpdate)
					r.Get("/update/{updateId}", h.GetUpdate)
					r.Post("/models", h.InitiateListModels)
					r.Get("/models/{requestId}", h.GetModelListRequest)
					r.Post("/local-skills", h.InitiateListLocalSkills)
					r.Get("/local-skills/{requestId}", h.GetLocalSkillListRequest)
					r.Post("/local-skills/import", h.InitiateImportLocalSkill)
					r.Get("/local-skills/import/{requestId}", h.GetLocalSkillImportRequest)
					r.Delete("/", h.DeleteAgentRuntime)
				})
			})

			// Tasks (user-facing, with ownership check)
			r.Post("/api/tasks/{taskId}/cancel", h.CancelTaskByUser)

			// Workspace-wide agent task snapshot for presence derivation:
			// every active task + each agent's most recent terminal task.
			r.Get("/api/agent-task-snapshot", h.ListWorkspaceAgentTaskSnapshot)

			// Workspace-wide daily agent activity (last 30d, anchored on
			// completed_at). Backs the Agents-list sparkline (trailing 7d
			// slice) AND the agent detail "Last 30 days" panel.
			r.Get("/api/agent-activity-30d", h.GetWorkspaceAgentActivity30d)

			// Workspace-wide 30-day run counts per agent for the Agents-list RUNS column.
			r.Get("/api/agent-run-counts", h.GetWorkspaceAgentRunCounts)

			r.Route("/api/chat/sessions", func(r chi.Router) {
				r.Post("/", h.CreateChatSession)
				r.Get("/", h.ListChatSessions)
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Get("/", h.GetChatSession)
					r.Delete("/", h.DeleteChatSession)
					r.Post("/messages", h.SendChatMessage)
					r.Get("/messages", h.ListChatMessages)
					r.Get("/pending-task", h.GetPendingChatTask)
					r.Post("/read", h.MarkChatSessionRead)
				})
			})
			r.Get("/api/chat/pending-tasks", h.ListPendingChatTasks)

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
			})

			// Notification preferences
			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreferences)
				r.Put("/", h.UpdateNotificationPreferences)
			})
		})
	})

	return r
}

// membershipChecker implements realtime.MembershipChecker using database queries.
type membershipChecker struct {
	queries *db.Queries
}

func (mc *membershipChecker) IsMember(ctx context.Context, userID, workspaceID string) bool {
	_, err := mc.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	return err == nil
}

// patResolver implements realtime.PATResolver using database queries.
// patCache is shared with the Auth and DaemonAuth middlewares so a token
// revoke through any path invalidates the cache for all of them. Nil
// cache is supported and degrades to direct DB lookups.
type patResolver struct {
	queries *db.Queries
	cache   *auth.PATCache
}

func (pr *patResolver) ResolveToken(ctx context.Context, token string) (string, bool) {
	hash := auth.HashToken(token)

	if userID, ok := pr.cache.Get(ctx, hash); ok {
		return userID, true
	}

	pat, err := pr.queries.GetPersonalAccessTokenByHash(ctx, hash)
	if err != nil {
		return "", false
	}

	userID := util.UUIDToString(pat.UserID)

	var expiresAt time.Time
	if pat.ExpiresAt.Valid {
		expiresAt = pat.ExpiresAt.Time
	}
	pr.cache.Set(ctx, hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))

	// Cache miss = first WS auth in this TTL window. Refresh last_used_at;
	// subsequent connects within the window skip the write.
	go pr.queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)

	return userID, true
}

// parseUUID is a thin alias for util.MustParseUUID. Call sites here are all
// internal round-trips of DB-sourced UUIDs (e.g. issue.ID, e.ActorID), so an
// invalid value indicates a programming error and should panic loudly.
func parseUUID(s string) pgtype.UUID {
	return util.MustParseUUID(s)
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}
