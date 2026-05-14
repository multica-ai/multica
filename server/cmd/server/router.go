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
	// CEREBRO-PATCH(cerebro-account-routes): JEH-921 account handler import
	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
	cerebrochannels "github.com/multica-ai/multica/server/internal/cerebro/channels"
	// CEREBRO-PATCH(cerebro-credentials-routes): JEH-1196 credential registry handler import
	cerebrocredentials "github.com/multica-ai/multica/server/internal/cerebro/credentials"
	// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard handler import
	cerebrodashboard "github.com/multica-ai/multica/server/internal/cerebro/dashboard"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/feature_flags"
	// CEREBRO-PATCH(cerebro-groups-routes): JEH-721 group handler import
	cerebrogroups "github.com/multica-ai/multica/server/internal/cerebro/groups"
	// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant control plane handler import
	cerebrogrants "github.com/multica-ai/multica/server/internal/cerebro/grants"
	// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 permission model handler import
	cerebrogrouppermissions "github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	cerebroinbox "github.com/multica-ai/multica/server/internal/cerebro/inbox"
	cerebronotifications "github.com/multica-ai/multica/server/internal/cerebro/notifications"
	// CEREBRO-PATCH(references-routes): JEH-837 issue references handler import.
	cerebroreferences "github.com/multica-ai/multica/server/internal/cerebro/references"
	// CEREBRO-PATCH(router-runtime-pause): cerebro runtime pause/unpause service.
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	// CEREBRO-PATCH(cerebro-tasks-route): JEH-900 tasks page handler import
	cerebrotasks "github.com/multica-ai/multica/server/internal/cerebro/tasks"
	// CEREBRO-PATCH(sharetoken-routes): JEH-1076 public-link share-token handler import
	cerebrosharetoken "github.com/multica-ai/multica/server/internal/cerebro/sharetoken"
	// CEREBRO-PATCH(cerebro-workflows-routes): JEH-1047 workflow engine REST handler import
	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
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
//
// rdb is optional: when non-nil the runtime local-skill request stores are
// swapped for Redis-backed implementations so multiple API nodes share the
// same pending queue (required for multi-node prod). This should be a request
// path Redis client, not the realtime relay's blocking read client. A nil rdb
// keeps the default in-memory stores which are fine for single-node dev and
// tests.
//
// CEREBRO-PATCH(router-push-service): pushSvc threaded through for Web Push
// notification handlers. Pass nil in tests that don't exercise the push path.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client, pushSvc *service.PushService) chi.Router {
	return NewRouterWithOptions(pool, hub, bus, analyticsClient, rdb, pushSvc, RouterOptions{})
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
	// CEREBRO-PATCH(cerebro-workflows-webhook-ingress): inbound webhook
	// handler needs the engine's Service to Execute a specific row outside
	// the bus. When nil, the route is registered with a 503 stub so the
	// router still builds for tests that don't wire the engine.
	WorkflowService *cerebroworkflows.Service
}

func NewRouterWithOptions(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client, pushSvc *service.PushService, opts RouterOptions) chi.Router {
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
		ScannerDiscoveryToken:         os.Getenv("MULTICA_SCANNER_DISCOVERY_TOKEN"),
// CEREBRO-PATCH(router): persona integration additions.
	}
	// CEREBRO-PATCH(handler-push-service): pushSvc passed into handler so
	// Web Push delivery shares the same connection pool and config as
	// notification listeners.
	h := handler.New(queries, pool, hub, bus, emailSvc, pushSvc, store, cfSigner, analyticsClient, signupConfig, daemonHub)
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

	// CEREBRO: feature-flag handler kept in dedicated package so upstream-merges
	// don't conflict on router wiring.
	cerebroQueries := cerebrodb.New(pool)
	featureFlagsHandler := feature_flags.New(cerebroQueries, bus)
	// CEREBRO-PATCH(router-channel-listen): wire the cerebro channel-listen
	// service into the upstream handler so the comment trigger path can
	// dispatch always-listening agents in channels.
	channelListenSvc := cerebrochannels.New(cerebroQueries, queries, h.TaskService, bus)
	h.ChannelListen = channelListenSvc
	// CEREBRO-PATCH(cerebro-inbox-routes): cerebro-only inbox handlers
	// (ListActiveIssueTasks; future folders/archive endpoints) live in their
	// own package so this wiring line is the only conflict surface upstream
	// merges hit.
	cerebroNotificationsHandler := cerebronotifications.New(queries)
	// CEREBRO-PATCH(cerebro-inbox-routes): mounts cerebro-only inbox actions
	// (mute/unmute/mark-unread). Adding new endpoints here keeps the conflict
	// surface to a single line per cerebro inbox feature.
	cerebroInboxHandler := cerebroinbox.New(queries, cerebroQueries)
	// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard handler instance
	cerebroDashboardHandler := cerebrodashboard.New(cerebroQueries, queries)
	// CEREBRO-PATCH(cerebro-groups-routes): JEH-721 workspace groups handler
	cerebroGroupsHandler := cerebrogroups.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 group permission model handler
	cerebroGroupPermissionsHandler := cerebrogrouppermissions.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant control plane handler + JEH-1212 upstream queries for subject validation
	cerebroGrantsHandler := cerebrogrants.NewHandler(cerebrogrants.New(cerebroQueries, queries, pool, bus)) // CEREBRO-PATCH(cerebro-grants-routes): JEH-1213
	// CEREBRO-PATCH(router-group-permissions-seam): JEH-1009 wire capability gate into the upstream handler
	h.GroupPermissions = cerebrogrouppermissions.NewHandlerSeam(cerebroGroupPermissionsHandler.Service)
	// CEREBRO-PATCH(cerebro-account-routes): JEH-921 workspace accounts handler
	cerebroAccountHandler := cerebroaccount.New(cerebroQueries, bus)
	// CEREBRO-PATCH(cerebro-credentials-routes): JEH-1196/1197 credential registry handler — cipher loaded from MULTICA_CREDENTIALS_KEY, governance policy wired via newCredentialsPolicy (owner-only when persona env is unset).
	cerebroCredentialsHandler := cerebrocredentials.New(cerebroQueries, cerebrocredentials.MustNewCipherFromEnv(), bus).WithPolicy(newCredentialsPolicy(queries))
	// CEREBRO-PATCH(references-routes): JEH-837 issue references handler instance.
	cerebroReferencesHandler := cerebroreferences.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(router-runtime-pause): mount cerebro pause/unpause service so
	// PauseRuntime / UnpauseRuntime in runtime_pause_cerebro.go can delegate to it.
	runtimePauseSvc := cerebroruntime.New(cerebroQueries, h.TaskService, bus)
	h.RuntimePause = runtimePauseSvc
	// CEREBRO-PATCH(router-auto-pause-on-failure): wire cerebro auto-pause
	// hook into TaskService so FailTask can pause on provider-error signals.
	h.TaskService.AutoPause = runtimePauseSvc
	// CEREBRO-PATCH(router-runtime-account): JEH-997 mount daemon-driven account
	// registration so RecordRuntimeAccount in runtime_account_cerebro.go can
	// delegate to the cerebro service via the RuntimeAccountInvoker seam.
	h.RuntimeAccount = cerebroruntime.NewAccountService(cerebroQueries, cerebroAccountHandler.Service, bus)
	// CEREBRO-PATCH(router-persona-mask): JEH-1079 mount the field-level
	// redaction service. Falls through to no-op when persona env is
	// unset — handlers behave as before for non-persona deployments.
	h.PersonaMask = newPersonaMaskInvoker()
	// CEREBRO-PATCH(router-persona-mask-audit): JEH-1173 mount the
	// redaction-audit writer. Always wired (independent of persona env)
	// so a future persona enablement starts logging from the first read.
	h.PersonaMaskAudit = newPersonaMaskAuditWriter(cerebroQueries)
	// CEREBRO-PATCH(cerebro-tasks-route): JEH-900 tasks page handler instance
	cerebroTasksHandler := cerebrotasks.New(cerebroQueries)
	// CEREBRO-PATCH(sharetoken-routes): JEH-1076 public-link share-token handler
	cerebroShareTokenHandler := cerebrosharetoken.NewHandler(cerebroQueries, queries)
	// CEREBRO-PATCH(cerebro-workflows-routes): JEH-1047 workflow handler instance; JEH-1108 PR3 wires the engine Service so the test-only /_test/cron-sweep endpoint can fire the sweeper synchronously.
	cerebroWorkflowsHandler := cerebroworkflows.NewHandler(cerebroQueries).WithService(opts.WorkflowService)

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

	// CEREBRO-PATCH(runtime-setup-routes): public token-gated runtime setup
	// (install script + exchange) — used by the cerebro daemon bootstrap flow.
	r.Get("/install-runtime.sh", h.ServeInstallRuntimeScript)
	r.Post("/api/runtime-setup/exchange", h.ExchangeRuntimeSetupToken)

	// CEREBRO-PATCH(sharetoken-public-route): JEH-1076 anonymous public-link
	// visitor path. Persona-gated; persona's /v1/check is called without a
	// bearer token and the share-token id is recorded as anonymous_context.
	r.Get("/api/cerebro/public/share/{token}", cerebroShareTokenHandler.PublicGet)
	// CEREBRO-PATCH(cerebro-workflows-webhook-ingress): JEH-1108 PR 2 public inbound webhook endpoint. Token-in-URL is the auth surface; HMAC + timestamp window are layered defenses. Mounted OUTSIDE the auth-required groups by design. When opts.WorkflowService is nil (tests), the route returns 503.
	if opts.WorkflowService != nil {
		webhookInbound := cerebroworkflows.NewWebhookInboundHandler(cerebroQueries, opts.WorkflowService, queries)
		r.Post("/api/cerebro/workflows/webhook/{token}", webhookInbound.ServeHTTP)
	}
	// CEREBRO-PATCH(sharetoken-public-route): JEH-1076 anonymous public-link
	// visitor path. Persona-gated; persona's /v1/check is called without a
	// bearer token and the share-token id is recorded as anonymous_context.
	r.Get("/api/cerebro/public/share/{token}", cerebroShareTokenHandler.PublicGet)

	// Scanner discovery (cross-workspace, service-token gated). Persona's
	// scanner consumes this to enumerate runtimes + their tools so its
	// coverage report can flag ungoverned tools per runtime.
	r.Get("/api/scanner-discovery/runtimes", h.GetScannerDiscoveryRuntimes)

	// Daemon API routes (require daemon token or valid user token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries, patCache, daemonTokenCache))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/ws", h.DaemonWebSocket)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)
		r.Get("/workspaces/{workspaceId}/agents/persona", h.ListWorkspacePersonaAgents)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)
		r.Post("/runtimes/{runtimeId}/models/{requestId}/result", h.ReportModelListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/{requestId}/result", h.ReportLocalSkillListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/import/{requestId}/result", h.ReportLocalSkillImportResult)
		r.Post("/runtimes/{runtimeId}/refresh-capabilities", h.RefreshRuntimeCapabilities)

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

		// CEREBRO-PATCH(cerebro-account-routes): JEH-998 daemon-only usage telemetry endpoint.
		r.Post("/accounts/{id}/usage", cerebroAccountHandler.UpdateUsage)
	})

	// === Task-scoped allowlist ===
	// Per-task tokens (mtt_) authenticate agents while they execute one
	// task and may only reach the small set of routes the agent needs:
	// read/update its own issue, post comments, read its own agent
	// config, look up workspace members for mentions, upload
	// attachments. These routes also accept normal user auth.
	//
	// Registered before the user-only protected group below so each
	// path lives in exactly one place — chi panics on duplicates.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// Attachment upload — agents drop screenshots/logs onto their
		// issue. AllowTaskScope is a no-op (the file resolves which
		// issue/comment via request body, not URL).
		r.With(middleware.AllowTaskScope).Post("/api/upload-file", h.UploadFile)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			r.With(middleware.AllowTaskScopeForAgent("id")).Get("/api/agents/{id}", h.GetAgent)

			// Issue routes registered flat (not via r.Route) so they
			// share the chi routing tree with the user-only sibling
			// group's nested r.Route("/api/issues/{id}", ...). Mixing
			// flat and nested registrations of the same prefix causes
			// chi to dispatch only one of the two trees and return 405
			// for methods registered in the other.
			//
			// Static GET siblings of /{id} (e.g. /search,
			// /child-progress) MUST also be registered here, before the
			// {id} line — once a flat /{id} GET exists in this tree,
			// chi greedily routes /search → GetIssue with id="search"
			// and the user-only nested registration becomes
			// unreachable. RequireUserScope keeps them user-only so a
			// task token still gets 403, not 200.
			r.With(middleware.RequireUserScope).Get("/api/issues/search", h.SearchIssues)
			r.With(middleware.RequireUserScope).Get("/api/issues/child-progress", h.ChildIssueProgress)
			issueScope := middleware.AllowTaskScopeForIssue("id")
			r.With(issueScope).Get("/api/issues/{id}", h.GetIssue)
			r.With(issueScope).Put("/api/issues/{id}", h.UpdateIssue)
			r.With(issueScope).Get("/api/issues/{id}/comments", h.ListComments)
			r.With(issueScope).Post("/api/issues/{id}/comments", h.CreateComment)
			// CEREBRO-PATCH(references-routes): JEH-837 issue-attached references.
			r.With(issueScope).Get("/api/issues/{id}/references", cerebroReferencesHandler.ListByIssue)
			r.With(issueScope).Post("/api/issues/{id}/references", cerebroReferencesHandler.Create)
		})

		// Workspace member listing for mention lookup. The wider
		// /api/workspaces/{id}/* tree is user-only because it carries
		// admin actions (invitations, member updates); we register
		// just /members here under AllowTaskScopeForWorkspace.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
			r.With(middleware.AllowTaskScopeForWorkspace("id")).Get("/api/workspaces/{id}/members", h.ListMembersWithUser)
		})
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))
		// Everything below this line is user-only. Task-scoped tokens
		// are rejected with 403; the allowlist group above is the
		// only place they may operate.
		r.Use(middleware.RequireUserScope)

		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		// CEREBRO-PATCH(me-profile-routes): cerebro user profile + preferences endpoints.
		r.Get("/api/me/profile", h.GetMyProfile)
		r.Put("/api/me/profile", h.UpsertMyProfile)
		r.Delete("/api/me/profile", h.DeleteMyProfile)
		r.Patch("/api/me/preferences", h.UpdateMyPreferences)
		r.Patch("/api/me/onboarding", h.PatchOnboarding)
		r.Post("/api/me/onboarding/complete", h.CompleteOnboarding)
		r.Post("/api/me/onboarding/cloud-waitlist", h.JoinCloudWaitlist)
		r.Post("/api/me/starter-content/import", h.ImportStarterContent)
		r.Post("/api/me/starter-content/dismiss", h.DismissStarterContent)
		r.Post("/api/cli-token", h.IssueCliToken)
		r.Post("/api/feedback", h.CreateFeedback)

		// CEREBRO-PATCH(me-inbox-cross-workspace): cross-workspace unread inbox
		// count for the OS app-icon badge. Outside the workspace-scoped tree
		// because the badge is single-icon and reflects every workspace.
		r.Get("/api/me/inbox/unread-count", h.CountUnreadInboxTotal)

		// CEREBRO-PATCH(web-push-routes): Web Push subscriptions (per-device,
		// per-user). The frontend reads the public key on load to decide
		// whether to show the subscribe UI.
		r.Get("/api/push/public-key", h.GetPushPublicKey)
		r.Get("/api/push/subscriptions", h.ListPushSubscriptions)
		r.Post("/api/push/subscribe", h.SubscribePush)
		r.Post("/api/push/unsubscribe", h.UnsubscribePush)
		// /api/upload-file is registered in the task-allowlist group above
		// so agents can upload attachments while running a task.

		// Persona pass-through for the agent settings UI. Returns []
		// when persona is not configured server-side so the dropdown
		// silently hides.
		r.Get("/api/persona/sandboxes", h.ListPersonaSandboxes)

		// CEREBRO-PATCH(persona-approvals): JEH-1078 approval inbox +
		// approve/deny proxy. List returns the calling Multica user's
		// "kræver din godkendelse" pool-matched requests; approve/deny
		// resolve on behalf of the user against persona.
		r.Get("/api/persona/approvals", h.ListPersonaApprovals)
		r.Post("/api/persona/approvals/{id}/approve", h.ApprovePersonaApproval)
		r.Post("/api/persona/approvals/{id}/deny", h.DenyPersonaApproval)


		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				// Member-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					// /members is registered in the task-allowlist group
					// above so agents can resolve mention targets.
					r.Post("/leave", h.LeaveWorkspace)
					r.Get("/invitations", h.ListWorkspaceInvitations)
					// CEREBRO-PATCH(feature-flags-routes): per-user feature-flag overrides
					r.Get("/feature-flags", featureFlagsHandler.List)
					r.Put("/feature-flags/{key}", featureFlagsHandler.Upsert)
					// CEREBRO-PATCH(cerebro-groups-routes): workspace group list (member-level).
					r.Get("/groups", cerebroGroupsHandler.List)
					// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant reads (any member).
					r.Get("/grants", cerebroGrantsHandler.List)
					r.Get("/grants/{grantId}", cerebroGrantsHandler.Get)
					// CEREBRO-PATCH(cerebro-account-routes): workspace accounts CRUD + JEH-998 controls patch.
					r.Get("/accounts", cerebroAccountHandler.List)
					r.Post("/accounts", cerebroAccountHandler.Create)
					r.Get("/accounts/{id}", cerebroAccountHandler.Get)
					r.Delete("/accounts/{id}", cerebroAccountHandler.Delete)
					r.Patch("/accounts/{id}/controls", cerebroAccountHandler.UpdateControls)
					// CEREBRO-PATCH(cerebro-credentials-routes): JEH-1196 credential registry routes (CRUD + reveal + rotate + bindings + audit).
					cerebroCredentialsHandler.Mount(r)
				})
				// Admin-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/pause-tasks", h.PauseWorkspaceTasks)
					r.Post("/members", h.CreateInvitation)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
						// Per-member budget toggle + spend roll-up
						// powering the member detail page.
						r.Patch("/budget-enforcement", h.PatchMemberBudgetEnforcement)
						r.Get("/usage", h.GetMemberUsage)
						// Restricted projects this member belongs to —
						// powers the Projects tab on the detail page.
						r.Get("/projects", h.ListMemberProjects)
					})
					r.Delete("/invitations/{invitationId}", h.RevokeInvitation)
					// CEREBRO-PATCH(cerebro-groups-routes): group create requires admin/owner (JEH-1172).
					r.Post("/groups", cerebroGroupsHandler.Create)
					// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant writes (admin/owner only).
					r.Post("/grants", cerebroGrantsHandler.Create)
					r.Patch("/grants/{grantId}", cerebroGrantsHandler.Update)
					r.Delete("/grants/{grantId}", cerebroGrantsHandler.Delete)
					// W4.6: audit feed for sandbox + admin actions.
					r.Get("/activity", h.ListWorkspaceActivity)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
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

			// CEREBRO-PATCH(cerebro-groups-routes): group CRUD and membership endpoints.
			r.Route("/api/groups", func(r chi.Router) {
				// Read-only: any workspace member.
				r.Get("/{id}", cerebroGroupsHandler.Get)
				r.Get("/{id}/members", cerebroGroupsHandler.ListMembers)
				// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 permission read endpoints.
				r.Get("/{id}/capabilities", cerebroGroupPermissionsHandler.ListCapabilities)
				r.Get("/{id}/runtimes", cerebroGroupPermissionsHandler.ListRuntimes)
				r.Get("/{id}/agents", cerebroGroupPermissionsHandler.ListAgents)
				// Writes: admin/owner only (JEH-1172).
				r.Group(func(r chi.Router) {
					// CEREBRO-PATCH(cerebro-groups-admin-gate): JEH-1172 group writes require admin/owner.
					r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
					r.Patch("/{id}", cerebroGroupsHandler.Update)
					r.Delete("/{id}", cerebroGroupsHandler.Delete)
					r.Post("/{id}/members", cerebroGroupsHandler.AddMember)
					r.Delete("/{id}/members/{userId}", cerebroGroupsHandler.RemoveMember)
					r.Post("/{id}/capabilities", cerebroGroupPermissionsHandler.SetCapability)
					r.Delete("/{id}/capabilities/{capability}", cerebroGroupPermissionsHandler.RemoveCapability)
					r.Post("/{id}/runtimes", cerebroGroupPermissionsHandler.AddRuntime)
					r.Delete("/{id}/runtimes/{runtimeId}", cerebroGroupPermissionsHandler.RemoveRuntime)
					r.Post("/{id}/agents", cerebroGroupPermissionsHandler.AddAgent)
					r.Delete("/{id}/agents/{agentId}", cerebroGroupPermissionsHandler.RemoveAgent)
				})
			})

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				// /search and /child-progress are registered flat in the
				// task-allowlist group above (with RequireUserScope) so
				// they share chi's routing tree with /{id} — see the
				// comment there.
				r.Get("/", h.ListIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/quick-create", h.QuickCreateIssue)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					// GET, PUT, /comments are registered in the task-allowlist
					// group above so agents can read and update their own
					// issue while running a task.
					r.Delete("/", h.DeleteIssue)
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
					r.Get("/artifacts", h.ListArtifactsForIssue)
					r.Get("/children", h.ListChildIssues)
					// CEREBRO-PATCH(issue-work-sessions): Claude Code MCP work-session listing per issue.
					r.Get("/work-sessions", h.ListWorkSessions)
					r.Get("/labels", h.ListLabelsForIssue)
					r.Post("/labels", h.AttachLabel)
					r.Delete("/labels/{labelId}", h.DetachLabel)
				})
			})

			// Task messages (user-facing, not daemon auth)
			r.Get("/api/tasks/{taskId}/messages", h.ListTaskMessagesByUser)

			// CEREBRO-PATCH(work-sessions-routes): Work sessions (Claude Code MCP integration)
			r.Route("/api/work-sessions", func(r chi.Router) {
				r.Post("/", h.CreateWorkSession)
				r.Get("/active", h.GetActiveWorkSession)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/messages", h.GetWorkSessionMessages)
					r.Put("/complete", h.CompleteWorkSession)
					r.Put("/name", h.UpdateWorkSessionName)
					r.Post("/messages", h.ReportWorkSessionMessages)
					r.Post("/resume", h.ResumeWorkSession)
					r.Post("/fork", h.ForkWorkSession)
				})
			})

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
				r.Get("/by-repo", h.GetProjectByRepo)
				// CEREBRO-PATCH(project-nesting-routes): fork-specific nested project endpoints.
				r.Get("/tree", h.ListProjectTree)
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					// CEREBRO-PATCH(project-access-control): cerebro project membership + access endpoints.
					r.Patch("/access", h.UpdateProjectAccess)
					r.Get("/members", h.ListProjectMembers)
					r.Post("/members", h.AddProjectMember)
					r.Delete("/members/{userId}", h.RemoveProjectMember)
					// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 project group access.
					r.Get("/group-access", cerebroGroupPermissionsHandler.ListProjectGroups)
					r.Post("/group-access", cerebroGroupPermissionsHandler.AddProjectGroup)
					r.Delete("/group-access/{groupId}", cerebroGroupPermissionsHandler.RemoveProjectGroup)
					r.Get("/artifacts", h.ListArtifactsForProject)
					r.Get("/resources", h.ListProjectResources)
					r.Post("/resources", h.CreateProjectResource)
					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
					r.Put("/parent", h.SetProjectParent)
					r.Put("/show-descendants", h.SetProjectShowDescendants)
					r.Get("/rollup-stats", h.GetProjectRollupStats)
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
				r.Post("/resolve", h.ResolveComment)
				r.Delete("/resolve", h.UnresolveComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			// Agents
			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)
				r.Route("/{id}", func(r chi.Router) {
					// GET is registered in the task-allowlist group above
					// so agents can read their own configuration.
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Post("/cancel-tasks", h.CancelAgentTasks)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)
					// CEREBRO-PATCH(agent-tools-routes): cerebro tool grant admin endpoints.
					r.Get("/tools", h.ListAgentTools)
					r.Route("/tools/{name}", func(r chi.Router) {
						r.Put("/", h.UpsertAgentTool)
					})
				})
			})

			// Skills
			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.Post("/", h.CreateSkill)
				r.Post("/import", h.ImportSkill)
				r.Get("/change-requests", h.ListPendingChangeRequests)
				r.Post("/change-requests/{crId}/review", h.ReviewSkillChangeRequest)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
					r.Put("/ownership", h.UpdateSkillOwnership)
					r.Get("/versions", h.ListSkillVersions)
					r.Get("/change-requests", h.ListSkillChangeRequests)
					r.Post("/change-requests", h.CreateSkillChangeRequest)
					r.Get("/forks", h.ListSkillForks)
					r.Post("/forks", h.CreateSkillFork)
				})
			})

			// Usage
			r.Route("/api/usage", func(r chi.Router) {
				r.Get("/daily", h.GetWorkspaceUsageByDay)
				r.Get("/summary", h.GetWorkspaceUsageSummary)
			})

			// Runtime setup token (workspace-scoped, generates one-line install command)
			r.Post("/api/runtime-setup/tokens", h.CreateRuntimeSetupToken)

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
					// CEREBRO-PATCH(runtime-sandbox): cerebro daemon sandbox toggle endpoint.
					r.Patch("/sandbox", h.UpdateAgentRuntimeSandbox)
					// CEREBRO-PATCH(router-runtime-pause): cerebro pause/unpause endpoints.
					r.Post("/pause", h.PauseRuntime)
					r.Post("/unpause", h.UnpauseRuntime)
					r.Post("/models", h.InitiateListModels)
					r.Get("/models/{requestId}", h.GetModelListRequest)
					r.Post("/local-skills", h.InitiateListLocalSkills)
					r.Get("/local-skills/{requestId}", h.GetLocalSkillListRequest)
					r.Post("/local-skills/import", h.InitiateImportLocalSkill)
					r.Get("/local-skills/import/{requestId}", h.GetLocalSkillImportRequest)
					r.Patch("/persona-sandbox", h.UpdateAgentRuntimePersonaSandbox)
					r.Delete("/", h.DeleteAgentRuntime)
				})
			})

			// Tasks (user-facing, with ownership check)
			r.Post("/api/tasks/{taskId}/cancel", h.CancelTaskByUser)

			// CEREBRO-PATCH(channels-routes): multi-party chat (issues with kind in channel,dm).
			r.Route("/api/channels", func(r chi.Router) {
				r.Get("/", h.ListChannels)
				r.Post("/", h.CreateChannel)
				r.Get("/{id}", h.GetChannel)
				r.Post("/{id}/read", h.MarkChannelRead)
				// CEREBRO-PATCH(channel-listen-routes): per-(channel, agent)
				// listen-mode toggle.
				r.Get("/{id}/agent-settings", channelListenSvc.ListSettings)
				r.Put("/{id}/agents/{agentId}/listen-mode", channelListenSvc.SetListenModeHandler)
				// CEREBRO-PATCH(channel-archive-routes): per-(channel, user)
				// archive flag (JEH-855/912).
				r.Post("/{id}/archive", channelListenSvc.ArchiveChannelHandler)
				r.Delete("/{id}/archive", channelListenSvc.UnarchiveChannelHandler)
			})

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
				// CEREBRO-PATCH(chat-search-route): JEH-901 Cmd+K chat session search.
				// Must precede the /{sessionId} subtree — once a flat /{sessionId}
				// GET exists, chi greedily routes /search → GetChatSession with
				// sessionId="search". Same trap as SearchIssues / SearchProjects.
				r.Get("/search", h.SearchChatSessions)
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Get("/", h.GetChatSession)
					r.Delete("/", h.DeleteChatSession)
					// CEREBRO-PATCH(chat-session-actions-routes): JEH-799 chat-session header.
					r.Patch("/", h.UpdateChatSession)
					r.Post("/convert-to-issue", h.ConvertChatSessionToIssue)
					r.Post("/messages", h.SendChatMessage)
					r.Get("/messages", h.ListChatMessages)
					r.Get("/pending-task", h.GetPendingChatTask)
					r.Post("/read", h.MarkChatSessionRead)
					r.Get("/usage", h.GetChatSessionUsage)
				})
			})
			r.Get("/api/chat/messages/{messageId}/attachments", h.ListChatMessageAttachments)
			r.Get("/api/chat/pending-tasks", h.ListPendingChatTasks)

			// Artifacts
			r.Route("/api/artifacts", func(r chi.Router) {
				r.Get("/", h.SearchArtifacts)
				r.Post("/", h.CreateArtifact)
				r.Get("/{id}", h.GetArtifact)
				r.Put("/{id}", h.UpdateArtifact)
				r.Put("/{id}/scope", h.UpdateArtifactScope)
				r.Put("/{id}/folder", h.MoveArtifactToFolder)
				r.Delete("/{id}", h.DeleteArtifact)
			})
			r.Route("/api/artifact-folders", func(r chi.Router) {
				r.Get("/", h.ListArtifactFolders)
				r.Post("/", h.CreateArtifactFolder)
				r.Put("/{id}", h.UpdateArtifactFolder)
				r.Delete("/{id}", h.DeleteArtifactFolder)
			})
			r.Post("/api/artifact-uploads", h.UploadArtifactFile)

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				// CEREBRO-PATCH(cerebro-inbox-routes): cerebro-only handler.
				r.Get("/active-issue-tasks", cerebroNotificationsHandler.ListActiveIssueTasks)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
				// CEREBRO-PATCH(cerebro-inbox-routes): cerebro-only mute/unread actions.
				r.Post("/{id}/mute", cerebroInboxHandler.MuteInboxItem)
				r.Delete("/{id}/mute", cerebroInboxHandler.UnmuteInboxItem)
				r.Post("/{id}/unread", cerebroInboxHandler.MarkInboxUnread)
				// CEREBRO-PATCH(cerebro-inbox-unarchive): JEH-1166 — unarchive action from archived view.
				r.Post("/{id}/unarchive", cerebroInboxHandler.UnarchiveInboxItem)

				// Notifications (route='notifications'). Single-item operations
				// (mark-read, archive) reuse the inbox endpoints above; only the
				// list/count/bulk endpoints are split.
				r.Get("/notifications", h.ListNotifications)
				r.Get("/notifications/unread-count", h.CountUnreadNotifications)
				r.Post("/notifications/mark-all-read", h.MarkAllNotificationsRead)
				r.Post("/notifications/archive-all", h.ArchiveAllNotifications)
			})

			// Notification preferences
			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreferences)
				r.Put("/", h.UpdateNotificationPreferences)
			})

			// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard overview endpoint
			r.Get("/api/cerebro/dashboard", cerebroDashboardHandler.Overview)
			// CEREBRO-PATCH(references-routes): JEH-837 reference-by-id mutations + reverse-lookup.
			r.Route("/api/cerebro/references", func(r chi.Router) {
				r.Get("/", cerebroReferencesHandler.ListByObject)
				r.Patch("/{refId}", cerebroReferencesHandler.Update)
				r.Delete("/{refId}", cerebroReferencesHandler.Delete)
			})
			// CEREBRO-PATCH(sharetoken-routes): JEH-1076 mint + revoke a
			// public share-token for an issue. Public GET is mounted on the
			// unauth tree above.
			r.Post("/api/cerebro/issues/{id}/share-tokens", cerebroShareTokenHandler.Create)
			r.Delete("/api/cerebro/share-tokens/{tokenId}", cerebroShareTokenHandler.Revoke)
			// CEREBRO-PATCH(cerebro-tasks-route): JEH-900 cross-agent tasks list endpoint
			r.Get("/api/cerebro/tasks", cerebroTasksHandler.List)
			// CEREBRO-PATCH(cerebro-workflows-routes): JEH-1047 workflow engine REST surface (PR 2/3).
			r.Route("/api/cerebro/workflows", func(r chi.Router) {
				r.Get("/", cerebroWorkflowsHandler.List)
				r.Post("/", cerebroWorkflowsHandler.Create)
				r.Get("/runs", cerebroWorkflowsHandler.Runs)
				r.Get("/{id}", cerebroWorkflowsHandler.Get)
				r.Put("/{id}", cerebroWorkflowsHandler.Update)
				r.Delete("/{id}", cerebroWorkflowsHandler.Delete)
				r.Post("/{id}/toggle", cerebroWorkflowsHandler.Toggle)
				r.Get("/{id}/runs", cerebroWorkflowsHandler.Runs)
				// CEREBRO-PATCH(cerebro-workflows-regenerate-token): JEH-1108 PR 2 token/secret regeneration endpoints + PR 3 test-only cron sweep hook (env-gated).
				r.Post("/{id}/regenerate-token", cerebroWorkflowsHandler.RegenerateInboundToken)
				r.Post("/{id}/regenerate-signing-secret", cerebroWorkflowsHandler.RegenerateInboundSigningSecret)
				r.Post("/{id}/regenerate-outbound-secret", cerebroWorkflowsHandler.RegenerateOutboundSecret)
				r.Post("/_test/cron-sweep", cerebroWorkflowsHandler.TestSweepCron)
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
