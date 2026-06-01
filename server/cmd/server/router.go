package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
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
	// CEREBRO-PATCH(cerebro-agent-passes-routes): JEH-1731 agent-pass admin handler import
	cerebroagentpass "github.com/multica-ai/multica/server/internal/cerebro/agentpass"
	cerebrochannels "github.com/multica-ai/multica/server/internal/cerebro/channels"
	// CEREBRO-PATCH(comments-move-to-subissue): JEH-1309 move-comment-to-sub-issue handler import.
	cerebrocomments "github.com/multica-ai/multica/server/internal/cerebro/comments"
	// CEREBRO-PATCH(cerebro-credentials-routes): JEH-1196 credential registry handler import
	cerebrocredentials "github.com/multica-ai/multica/server/internal/cerebro/credentials"
	// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard handler import
	cerebrodashboard "github.com/multica-ai/multica/server/internal/cerebro/dashboard"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	// CEREBRO-PATCH(cerebro-cost-optimization-routes): FIR-2325 per-workspace saving-mode handler import.
	cerebrocostoptimization "github.com/multica-ai/multica/server/internal/cerebro/cost_optimization"
	// CEREBRO-PATCH(cerebro-pricing-route): FIR-2471 service-to-service pricing pull for the registry's agent-trace costing.
	cerebropricing "github.com/multica-ai/multica/server/internal/cerebro/pricing"
	// CEREBRO-PATCH(cerebro-dictation-routes): JEH-729 streaming dictation proxy handler import.
	cerebrodictation "github.com/multica-ai/multica/server/internal/cerebro/dictation"
	"github.com/multica-ai/multica/server/internal/cerebro/feature_flags"
	// CEREBRO-PATCH(cerebro-groups-routes): JEH-721 group handler import
	cerebrogroups "github.com/multica-ai/multica/server/internal/cerebro/groups"
	// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant control plane handler import
	cerebrogrants "github.com/multica-ai/multica/server/internal/cerebro/grants"
	// CEREBRO-PATCH(cerebro-tool-policy-routes): FIR-2230 unified per-tool policy handler import
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	// CEREBRO-PATCH(cerebro-sandbox-profile-routes): FIR-2230 sandbox isolation profile catalog handler import
	cerebrosandboxprofile "github.com/multica-ai/multica/server/internal/cerebro/sandboxprofile"
	// CEREBRO-PATCH(cerebro-roles-routes): FIR-2130 role subject CRUD + assignment handler import
	cerebroroles "github.com/multica-ai/multica/server/internal/cerebro/roles"
	// CEREBRO-PATCH(cerebro-identity-routes): FIR-2523 Google Workspace identity-source handler import
	cerebroidentity "github.com/multica-ai/multica/server/internal/cerebro/identity"
	// CEREBRO-PATCH(cerebro-identity-login-sync): FIR-2662 on-demand Google Workspace group sync after login auto-provisioning.
	cerebroidentitysync "github.com/multica-ai/multica/server/internal/cerebro/identitysync"
	// CEREBRO-PATCH(cerebro-approvals-routes): FIR-2131 approval inbox handler import
	cerebroapprovals "github.com/multica-ai/multica/server/internal/cerebro/approvals"
	// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 permission model handler import
	cerebrogrouppermissions "github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	// CEREBRO-PATCH(cerebro-github-pr-heal): JEH-1919 PR-card self-heal service import.
	cerebrocommentguard "github.com/multica-ai/multica/server/internal/cerebro/commentguard" // CEREBRO-PATCH(router-comment-target-guard-import): FIR-2674.
	cerebrogithubprheal "github.com/multica-ai/multica/server/internal/cerebro/githubprheal"
	cerebroinbox "github.com/multica-ai/multica/server/internal/cerebro/inbox"
	cerebromentiongate "github.com/multica-ai/multica/server/internal/cerebro/mentiongate"
	cerebroprivateagentrun "github.com/multica-ai/multica/server/internal/cerebro/privateagentrun" // CEREBRO-PATCH(router-private-agent-run-request): FIR-2385.
	// CEREBRO-PATCH(references-routes): JEH-837 issue references handler import.
	cerebroreferences "github.com/multica-ai/multica/server/internal/cerebro/references"
	// CEREBRO-PATCH(router-runtime-pause): cerebro runtime pause/unpause service.
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	// CEREBRO-PATCH(router-semantic-search): FIR-2604 semantic Provider + worker.
	cerebrosemantic "github.com/multica-ai/multica/server/internal/cerebro/semantic"
	// CEREBRO-PATCH(router-runtime-tools-admin): JEH-1710 unified runtime tool admin service.
	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	// CEREBRO-PATCH(router-capability-register): FIR-2129 capability register service.
	cerebrocapabilityregistry "github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	// CEREBRO-PATCH(router-cloud-runtime-tool-scan): FIR-2284 server-side cloud-runtime tool scan.
	"github.com/multica-ai/multica/server/internal/cerebro/cloudtoolscan"
	// CEREBRO-PATCH(cerebro-tasks-route): JEH-900 tasks page handler import
	cerebrotasks "github.com/multica-ai/multica/server/internal/cerebro/tasks"
	// CEREBRO-PATCH(sharetoken-routes): JEH-1076 public-link share-token handler import
	cerebrosharetoken "github.com/multica-ai/multica/server/internal/cerebro/sharetoken"
	// CEREBRO-PATCH(cerebro-workflows-routes): JEH-1047 workflow engine REST handler import
	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
	// CEREBRO-PATCH(cerebro-status-models-routes): FIR-1550 workflow v2a status-model handler import
	cerebrostatusmodels "github.com/multica-ai/multica/server/internal/cerebro/statusmodels"
	// CEREBRO-PATCH(cerebro-sprints-routes): FIR-2666 project sprint handler import
	cerebrosprints "github.com/multica-ai/multica/server/internal/cerebro/sprints"
	// CEREBRO-PATCH(agent-avatar-generate): JEH-1563 AI avatar generation handler import
	cerebroagentavatar "github.com/multica-ai/multica/server/internal/cerebro/agent_avatar"
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

// parseTrustedProxies parses a comma-separated list of CIDR prefixes from the
// MULTICA_TRUSTED_PROXIES env var. Invalid entries are dropped with a single
// warn-line per entry rather than crashing the server — a typo in one CIDR
// shouldn't take the whole API down. Returns nil for empty input, which the
// rate limiter treats as "trust no proxy headers, use RemoteAddr only".
func parseTrustedProxies(raw string) []netip.Prefix {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			slog.Warn("MULTICA_TRUSTED_PROXIES: ignoring invalid CIDR",
				"value", s, "error", err)
			continue
		}
		out = append(out, p)
	}
	return out
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
		AllowSignup:              os.Getenv("ALLOW_SIGNUP") != "false",
		AllowedEmails:            splitAndTrim(os.Getenv("ALLOWED_EMAILS")),
		AllowedEmailDomains:      splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS")),
		DisableWorkspaceCreation: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
		PublicURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PUBLIC_URL")), "/"),
		TrustedProxies:           parseTrustedProxies(os.Getenv("MULTICA_TRUSTED_PROXIES")),
		CloudRuntimeFleetURL:     cloudRuntimeFleetURLFromEnv(),
		CloudRuntimeFleetTimeout: envDuration("MULTICA_CLOUD_FLEET_TIMEOUT", 35*time.Second),
		// CEREBRO-PATCH(router): persona integration additions.
	}
	// CEREBRO-PATCH(handler-push-service): pushSvc passed into handler so
	// Web Push delivery shares the same connection pool and config as
	// notification listeners.
	h := handler.New(queries, pool, hub, bus, emailSvc, pushSvc, store, cfSigner, analyticsClient, signupConfig, daemonHub)
	// CEREBRO-PATCH(orchestration-watchdog): FIR-2564 — periodic stall sweep so a hung agent/CI can't silently freeze an orchestration.
	go handler.NewOrchestrationWatchdog(h).Run(context.Background())
	if opts.DaemonWakeup != nil {
		h.TaskService.Wakeup = opts.DaemonWakeup
	}
	if rdb != nil {
		h.UpdateStore = handler.NewRedisUpdateStore(rdb)
		h.ModelListStore = handler.NewRedisModelListStore(rdb)
		h.LocalSkillListStore = handler.NewRedisLocalSkillListStore(rdb)
		h.LocalSkillImportStore = handler.NewRedisLocalSkillImportStore(rdb)
		h.LivenessStore = handler.NewRedisLivenessStore(rdb)
		h.WebhookRateLimiter = handler.NewRedisWebhookRateLimiter(rdb, handler.DefaultWebhookRateLimit())
		h.WebhookIPRateLimiter = handler.NewRedisWebhookIPRateLimiter(rdb, handler.DefaultWebhookIPRateLimit())
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
	h.MembershipCache = auth.NewMembershipCache(rdb)

	// Cloud PAT verifier: validates mcn_ tokens against Multica Cloud
	// Fleet. Returns nil when no Fleet URL is configured — the Auth /
	// DaemonAuth middlewares treat nil as "mcn_ not supported" and
	// reject with 401, instead of falling through to mul_/JWT paths.
	// Reuses MULTICA_CLOUD_FLEET_URL (the same URL the cloud-runtime
	// proxy uses) so a deployment doesn't need a second config knob.
	cloudPATVerifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{
		FleetBaseURL: signupConfig.CloudRuntimeFleetURL,
		Redis:        rdb,
	})

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
	h.CerebroQueries = cerebroQueries
	h.PullRequestLinkHealer = cerebrogithubprheal.New(cerebroQueries, queries) // CEREBRO-PATCH(cerebro-github-pr-heal): JEH-1919
	featureFlagsHandler := feature_flags.New(cerebroQueries, bus)
	// CEREBRO-PATCH(cerebro-cost-optimization-routes): FIR-2325 per-workspace saving-mode handler.
	costOptimizationHandler := cerebrocostoptimization.New(cerebroQueries, bus)
	// CEREBRO-PATCH(router-channel-listen): wire the cerebro channel-listen
	// service into the upstream handler so the comment trigger path can
	// dispatch always-listening agents in channels.
	channelListenSvc := cerebrochannels.New(cerebroQueries, queries, h.TaskService, bus)
	h.ChannelListen = channelListenSvc
	// CEREBRO-PATCH(cerebro-inbox-routes): mounts cerebro-only inbox actions
	// (mute/unmute/mark-unread). Adding new endpoints here keeps the conflict
	// surface to a single line per cerebro inbox feature.
	// CEREBRO-PATCH(cerebro-inbox-realtime): FIR-2394 event bus fans inbox metadata mutations out to other sessions; FIR-2385 task service backs the owner-run endpoint.
	cerebroInboxHandler := cerebroinbox.New(queries, cerebroQueries, bus, h.TaskService)
	// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard handler instance
	cerebroDashboardHandler := cerebrodashboard.New(cerebroQueries, queries)
	// CEREBRO-PATCH(cerebro-pricing-route): FIR-2471 pricing endpoint handler instance (service key from CEREBRO_PRICING_KEY).
	cerebroPricingHandler := cerebropricing.New(os.Getenv("CEREBRO_PRICING_KEY"))
	// CEREBRO-PATCH(cerebro-github-pr-link-route): FIR-2568 poll-based PR↔issue linking endpoint instance (service key from CEREBRO_GITHUB_LINK_KEY; default workspace slug from CEREBRO_GITHUB_LINK_WORKSPACE_SLUG). Logic in handler/github_pr_link_cerebro.go.
	cerebroGitHubPRLinkHandler := handler.NewCerebroGitHubPRLinkHandler(h, os.Getenv("CEREBRO_GITHUB_LINK_KEY"), os.Getenv("CEREBRO_GITHUB_LINK_WORKSPACE_SLUG"))
	// CEREBRO-PATCH(cerebro-dictation-routes): JEH-729 workspace-scoped WebSocket proxy; inference deploy stays out of this slice.
	cerebroDictationHandler := cerebrodictation.NewFromEnv(queries, patCache)
	// CEREBRO-PATCH(cerebro-groups-routes): JEH-721 workspace groups handler
	cerebroGroupsHandler := cerebrogroups.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(cerebro-group-permissions-routes): JEH-1008 group permission model handler
	cerebroGroupPermissionsHandler := cerebrogrouppermissions.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant control plane handler + JEH-1212 upstream queries for subject validation
	cerebroGrantsHandler := cerebrogrants.NewHandler(cerebrogrants.New(cerebroQueries, queries, pool, bus)) // CEREBRO-PATCH(cerebro-grants-routes): JEH-1213
	cerebroRolesHandler := cerebroroles.New(cerebroQueries, queries, bus)                                   // CEREBRO-PATCH(cerebro-roles-routes): FIR-2130 role subject handler
	// CEREBRO-PATCH(cerebro-identity-handler): FIR-2523 Google Workspace identity-source handler + provisioner seam.
	var cerebroIdentityGroupSyncer *cerebroidentitysync.Syncer
	if syncer, ok, err := cerebroidentitysync.NewSyncerFromEnv(context.Background(), cerebroQueries, queries); err != nil {
		slog.Warn("cerebro identity login group sync disabled: BigQuery init failed", "error", err)
	} else if ok {
		cerebroIdentityGroupSyncer = syncer
	}
	cerebroIdentityService := cerebroidentity.NewWithGroupSyncer(cerebroQueries, queries, cerebroIdentityGroupSyncer)
	cerebroIdentityHandler := cerebroidentity.NewHandler(cerebroIdentityService)
	h.IdentityProvisioner = cerebroIdentityService
	// CEREBRO-PATCH(cerebro-tool-policy-routes): FIR-2230 unified per-tool policy table handler (data layer the permission screen reads from).
	cerebroToolPolicyHandler := cerebrotoolpolicy.NewHandler(cerebrotoolpolicy.NewStore(pool))
	// CEREBRO-PATCH(cerebro-sandbox-profile-routes): FIR-2230 sandbox isolation profile catalog handler.
	cerebroSandboxProfileHandler := cerebrosandboxprofile.NewHandler()
	// CEREBRO-PATCH(cerebro-approvals-routes): FIR-2131 approval inbox handler — materialises permission-engine needs_approval verdicts into a human inbox.
	cerebroApprovalsHandler := cerebroapprovals.NewHandler(cerebroapprovals.New(cerebroQueries, pool, bus))
	// CEREBRO-PATCH(router-group-permissions-seam): JEH-1009 wire capability gate into the upstream handler
	h.GroupPermissions = cerebrogrouppermissions.NewHandlerSeam(cerebroGroupPermissionsHandler.Service)
	mentionGate := cerebromentiongate.New(queries, cerebroGroupPermissionsHandler.Service) // CEREBRO-PATCH(router-mention-trigger-gate): JEH-1917.
	h.MentionTriggerGate = mentionGate
	channelListenSvc.AgentTriggerGate = mentionGate.ChannelListenGate()
	if os.Getenv("CEREBRO_COMMENT_TARGET_GUARD_ENABLED") == "true" { // CEREBRO-PATCH(router-comment-target-guard): FIR-2674 — opt-in kill-switch; off until agents are taught to always include a target.
		h.CommentTargetGuard = cerebrocommentguard.New()
	}
	// CEREBRO-PATCH(router-private-agent-run-request): FIR-2385 — member tag of an unowned private agent → owner inbox run-request.
	h.PrivateAgentRunRequester = cerebroprivateagentrun.New(cerebroQueries, bus)
	// CEREBRO-PATCH(cerebro-account-routes): JEH-921 workspace accounts handler
	cerebroAccountHandler := cerebroaccount.New(cerebroQueries, bus)
	// CEREBRO-PATCH(router-approval-gate): FIR-2586 build the shared approval gate
	// once (nil when CEREBRO_APPROVAL_GATE_ENABLED is off) and wire it into every
	// enforcement point so an "Ask" lands in the one /approvals inbox: daemon repo
	// checkout (h.ApprovalGate) and credential governance (newCredentialsPolicy).
	sharedApprovalGate := cerebroruntime.BuildApprovalGate(cerebroQueries, pool, bus)
	h.ApprovalGate = sharedApprovalGate
	// CEREBRO-PATCH(router-semantic-search): FIR-2604 wire semantic provider + worker.
	semanticCfg := cerebrosemantic.LoadConfig()
	h.SemanticSearch = handler.NewSemanticSearch(pool, semanticCfg.BuildProvider(), semanticCfg)
	// CEREBRO-PATCH(cerebro-credentials-routes): JEH-1196/1197 credential registry handler — cipher loaded from MULTICA_CREDENTIALS_KEY, governance policy wired via newCredentialsPolicy (Persona/Multica cut-over controlled by MULTICA_PERMISSION_ENGINE).
	cerebroCredentialsCipher := cerebrocredentials.MustNewCipherFromEnv()
	cerebroCredentialsHandler := cerebrocredentials.New(cerebroQueries, cerebroCredentialsCipher, bus).WithPolicy(newCredentialsPolicy(cerebroQueries, queries, sharedApprovalGate))
	// CEREBRO-PATCH(router-infisical-provisioner): FIR-2192 scoped-per-user
	// Infisical machine identity provisioner. Reads admin credentials +
	// project/org IDs from INFISICAL_ADMIN_* env vars. Nil when unset so dev
	// + self-host instances without Infisical still boot.
	if provisioner := newInfisicalProvisioner(cerebroQueries, cerebroCredentialsCipher); provisioner != nil {
		h.InfisicalProvisioner = provisioner
	}
	// CEREBRO-PATCH(references-routes): JEH-837 issue references handler instance.
	cerebroReferencesHandler := cerebroreferences.New(cerebroQueries, queries, bus)
	// CEREBRO-PATCH(comments-move-to-subissue): JEH-1309 lift a comment thread into a sub-issue.
	cerebroCommentsHandler := cerebrocomments.New(queries, pool, bus)
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
	// CEREBRO-PATCH(router-tool-meta): JEH-1353 — wire tool display metadata so
	// the admin API can return names + descriptions for every registered tool.
	{
		rawMeta := cerebroruntime.AllBuiltinToolMeta()
		items := make([]handler.CerebroToolItem, len(rawMeta))
		for i, m := range rawMeta {
			items[i] = handler.CerebroToolItem{Name: m.Name, Description: m.Description, Status: m.Status} // CEREBRO-PATCH(router-tool-status): expose explicit exclusion status to the tools API.
		}
		h.SetCerebroToolMeta(items)
	}
	// CEREBRO-PATCH(router-runtime-tools-admin): JEH-1710 wire the unified
	// runtime tool admin service (per-runtime tool inventory + group/user
	// grants + per-agent overrides) and the daemon-side scan ingest seam.
	runtimeToolsSvc := runtimetools.New(pool)
	h.SetRuntimeToolsAdmin(newRuntimeToolsAdminAdapter(runtimeToolsSvc))
	h.SetRuntimeToolsScan(newRuntimeToolsScanAdapter(runtimeToolsSvc))
	// CEREBRO-PATCH(router-capability-register): FIR-2129 wire capability register API.
	capabilityRegisterSvc := cerebrocapabilityregistry.New(pool)
	h.SetCapabilityRegister(newCapabilityRegisterAdapter(capabilityRegisterSvc))
	// CEREBRO-PATCH(router-cloud-runtime-tool-scan): FIR-2284 — server-side "Scan now"
	// for cloud runtimes (no daemon): record the gateway's callable built-in tool
	// surface into the capability register (the unified table's source) + legacy inventory.
	h.SetCloudRuntimeToolScanner(cloudtoolscan.New(capabilityRegisterSvc, runtimeToolsSvc, callableCloudToolMeta()))
	// CEREBRO-PATCH(cerebro-tasks-route): JEH-900 tasks page handler instance
	cerebroTasksHandler := cerebrotasks.New(cerebroQueries)
	// CEREBRO-PATCH(sharetoken-routes): JEH-1076 public-link share-token handler
	cerebroShareTokenHandler := cerebrosharetoken.NewHandler(cerebroQueries, queries)
	// CEREBRO-PATCH(cerebro-workflows-routes): JEH-1047 workflow handler instance; JEH-1108 PR3 wires the engine Service so the test-only /_test/cron-sweep endpoint can fire the sweeper synchronously.
	cerebroWorkflowsHandler := cerebroworkflows.NewHandler(cerebroQueries).WithService(opts.WorkflowService)
	// CEREBRO-PATCH(cerebro-status-models-routes): FIR-1550 v2b — pass upstream queries so per-issue custom status mirrors onto the upstream issue row, and pass the pool so the two writes commit atomically (Mia review).
	cerebroStatusModelsHandler := cerebrostatusmodels.NewHandler(cerebroQueries).WithUpstream(queries).WithTx(pool)
	// CEREBRO-PATCH(custom-status-resolver-wire): FIR-1550 v2b — UpdateIssue invokes the resolver.
	h.CustomStatusResolver = cerebroStatusModelsHandler
	// CEREBRO-PATCH(agent-avatar-generate): JEH-1563 AI avatar generation handler instance; FIR-2049 pass queries so avatar reads gateway creds from workspace settings
	cerebroAgentAvatarHandler := cerebroagentavatar.New(store, queries)
	// CEREBRO-PATCH(cerebro-sprints-routes): FIR-2666 project sprint handler instance
	cerebroSprintsHandler := cerebrosprints.NewHandler(cerebroQueries)

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
	// CEREBRO-PATCH(cerebro-dictation-ws-auth): browsers cannot attach Authorization headers to WebSocket upgrades.
	r.Get("/api/workspaces/{id}/cerebro/dictation/stream", cerebroDictationHandler.Stream)

	// Local file serving (when using local storage)
	if local, ok := store.(*storage.LocalStorage); ok {
		r.Get("/uploads/*", func(w http.ResponseWriter, r *http.Request) {
			file := strings.TrimPrefix(r.URL.Path, "/uploads/")
			local.ServeFile(w, r, file)
		})
	}

	// Auth (public) — per-IP rate limiting.
	if rdb == nil {
		slog.Warn("rate limiting disabled: REDIS_URL not configured")
	}
	trustedProxies := middleware.ParseTrustedProxies(os.Getenv("RATE_LIMIT_TRUSTED_PROXIES"))
	authRL := middleware.RateLimit(rdb, envPositiveInt("RATE_LIMIT_AUTH", 5), time.Minute, trustedProxies)
	authVerifyRL := middleware.RateLimit(rdb, envPositiveInt("RATE_LIMIT_AUTH_VERIFY", 20), time.Minute, trustedProxies)
	contactSalesRL := middleware.RateLimit(rdb, envPositiveInt("RATE_LIMIT_CONTACT_SALES", 5), time.Hour, trustedProxies)
	r.With(authRL).Post("/auth/send-code", h.SendCode)
	r.With(authVerifyRL).Post("/auth/verify-code", h.VerifyCode)
	r.With(authRL).Post("/auth/google", h.GoogleLogin)
	r.Post("/auth/logout", h.Logout)

	// Public API
	r.Get("/api/config", h.GetConfig)
	// CEREBRO-PATCH(cerebro-pricing-route): FIR-2471 service-to-service price-table read for the registry's hourly pricing pull. Outside the user-auth group on purpose — the caller is a backend service that presents CEREBRO_PRICING_KEY as a Bearer token (loopback-only when the key is unset).
	r.Get("/api/cerebro/pricing", cerebroPricingHandler.Get)
	// CEREBRO-PATCH(cerebro-github-pr-link-route): FIR-2568 service-to-service PR-link write for the registry's poll-based scanner. Outside the user-auth group on purpose — the caller is a backend service presenting CEREBRO_GITHUB_LINK_KEY as a Bearer token (loopback-only when the key is unset).
	r.Post("/api/cerebro/github/pull-requests", cerebroGitHubPRLinkHandler.Post)
	r.With(contactSalesRL).Post("/api/contact-sales", h.CreateContactSales)

	// Webhook ingress for autopilots. Outside the authenticated group on
	// purpose: the bearer token in the URL path IS the credential. Workspace
	// context is derived from the trigger row, never from request headers.
	r.Post("/api/webhooks/autopilots/{token}", h.HandleAutopilotWebhook)
	// CEREBRO-PATCH(runtime-setup-routes): public token-gated runtime setup
	r.Post("/api/runtime-setup/exchange", h.ExchangeRuntimeSetupToken)
	r.Get("/install-runtime.sh", h.ServeInstallRuntimeScript)
	// CEREBRO-PATCH(sharetoken-public-route): JEH-1076 anonymous public-link
	// CEREBRO-PATCH(cerebro-workflows-webhook-ingress): JEH-1108 PR 2 public inbound webhook endpoint. Token-in-URL is the auth surface; HMAC + timestamp window are layered defenses. Mounted OUTSIDE the auth-required groups by design. When opts.WorkflowService is nil (tests), the route returns 503.
	// CEREBRO-PATCH(sharetoken-public-route): JEH-1076 anonymous public-link
	// GitHub App webhook (no Multica auth — requests are authenticated via
	// HMAC-SHA256 signature in the handler) and post-install setup callback.
	r.Post("/api/webhooks/github", h.HandleGitHubWebhook)
	r.Get("/api/github/setup", h.GitHubSetupCallback)
	// Stripe webhook (no Multica auth — Stripe signs the raw body
	// with a shared secret, the multica-cloud upstream verifies. We
	// only forward the bytes + the Stripe-Signature header; see
	// HandleCloudBillingStripeWebhook for the rationale).
	r.Post("/api/webhooks/stripe", h.HandleCloudBillingStripeWebhook)

	// Daemon API routes (require daemon token or valid user token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries, patCache, daemonTokenCache, cloudPATVerifier))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/ws", h.DaemonWebSocket)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)
		r.Post("/workspaces/{workspaceId}/repo/check", h.CheckDaemonRepoCapability)          // CEREBRO-PATCH(daemon-repo-grants): FIR-2512
		r.Get("/workspaces/{workspaceId}/repo/check/{approvalId}", h.PollDaemonRepoApproval) // CEREBRO-PATCH(daemon-repo-approval-gate): FIR-2586 poll a repo-checkout approval
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
		r.Post("/tasks/{taskId}/wait-local-directory", h.MarkTaskWaitingLocalDirectory)
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
		// CEREBRO-PATCH(runtime-tools-scan-routes): JEH-1710 daemon MCP scan
		// — fetch the runtime's mcp-config and ingest a tools/list result.
		r.Get("/runtimes/{runtimeId}/mcp-config", h.GetRuntimeMcpConfig)
		r.Post("/runtimes/{runtimeId}/tool-scan", h.IngestRuntimeToolScan)
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
		r.Use(middleware.Auth(queries, patCache, cloudPATVerifier))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// Attachment upload — agents drop screenshots/logs onto their
		// issue. AllowTaskScope is a no-op (the file resolves which
		// issue/comment via request body, not URL).
		r.With(middleware.AllowTaskScope).Post("/api/upload-file", h.UploadFile)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			r.With(middleware.RequireUserScope).Get("/api/agents/backfill-avatars", cerebroAgentAvatarHandler.BackfillStatus) // CEREBRO-PATCH(agent-avatar-backfill): keep static GET before /api/agents/{id}.
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
			r.With(middleware.RequireUserScope).Get("/api/issues/grouped", h.ListGroupedIssues)
			issueScope := middleware.AllowTaskScopeForIssue("id")
			r.With(issueScope).Get("/api/issues/{id}", h.GetIssue)
			// CEREBRO-PATCH(issue-context-bundle-route): FIR-2384 — bundled issue+comments+members+labels read for the `multica issue context` cost saving.
			r.With(issueScope).Get("/api/issues/{id}/context", h.GetIssueContext)
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
		r.Use(middleware.Auth(queries, patCache, cloudPATVerifier))
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
		// DEPRECATED — shim routes for desktop < v3 during the rollout
		// window. v3 frontend creates the Helper agent + starter issue
		// via generic CreateAgent / CreateIssue and only calls /complete
		// here. Remove once X-Client-Version telemetry confirms zero
		// pre-v3 desktops are still calling these. Handlers live in
		// server/internal/handler/onboarding_shim.go.
		r.Post("/api/me/onboarding/runtime-bootstrap", h.BootstrapOnboardingRuntime)
		r.Post("/api/me/onboarding/no-runtime-bootstrap", h.BootstrapOnboardingNoRuntime)
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
		// CEREBRO-PATCH(runtime-setup-routes): current web/desktop clients use
		// workspace headers; older QA/docs use the workspace-scoped path below.
		r.With(middleware.RequireWorkspaceMember(queries)).Post("/api/runtime-setup/tokens", h.CreateRuntimeSetupToken)
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
					// Listing GitHub installations is member-visible so the
					// integrations tab no longer renders blank for non-admins;
					// the handler strips the management handle and adds a
					// can_manage hint so the UI can gate connect/disconnect.
					r.Get("/github/installations", h.ListGitHubInstallations)
					// CEREBRO-PATCH(feature-flags-routes): per-user feature-flag overrides
					r.Get("/feature-flags", featureFlagsHandler.List)
					r.Put("/feature-flags/{key}", featureFlagsHandler.Upsert)
					// CEREBRO-PATCH(cerebro-cost-optimization-routes): FIR-2325 saving-mode read (any member).
					r.Get("/cost-optimization", costOptimizationHandler.List)
					// CEREBRO-PATCH(cerebro-cost-optimization-dashboard): FIR-2325 phase-5 savings dashboard (any member).
					r.Get("/cost-optimization/dashboard", costOptimizationHandler.Dashboard)
					// CEREBRO-PATCH(cerebro-cost-optimization-holdout): FIR-2640 per-saving holdout share read (any member).
					r.Get("/cost-optimization/holdout", costOptimizationHandler.ListHoldout)
					// CEREBRO-PATCH(cerebro-groups-routes): workspace group list (member-level).
					r.Get("/groups", cerebroGroupsHandler.List)
					// CEREBRO-PATCH(cerebro-roles-routes): FIR-2130 workspace role list (member-level).
					r.Get("/roles", cerebroRolesHandler.List)
					// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant reads (any member).
					r.Get("/grants", cerebroGrantsHandler.List)
					r.Get("/grants/audit", cerebroGrantsHandler.Audit) // CEREBRO-PATCH(persona-permissions-audit): expose grant audit before {grantId}.
					r.Post("/grants/evaluate", cerebroGrantsHandler.Evaluate)
					r.Get("/grants/{grantId}", cerebroGrantsHandler.Get)
					// CEREBRO-PATCH(cerebro-tool-policy-routes): FIR-2230 per-tool policy table read (any member).
					r.Get("/tool-policy", cerebroToolPolicyHandler.Table)
					// CEREBRO-PATCH(cerebro-sandbox-profile-routes): FIR-2230 sandbox isolation profile catalog (any member).
					r.Get("/sandbox-profiles", cerebroSandboxProfileHandler.List)
					// CEREBRO-PATCH(cerebro-approvals-routes): FIR-2131 approval inbox reads (any member). /audit before /{approvalId} so it is not shadowed.
					r.Get("/approvals", cerebroApprovalsHandler.List)
					r.Get("/approvals/audit", cerebroApprovalsHandler.Audit)
					r.Get("/approvals/{approvalId}", cerebroApprovalsHandler.Get)
					// CEREBRO-PATCH(runtime-setup-routes): legacy workspace-scoped setup-token mint.
					r.Post("/runtime-setup-token", h.CreateRuntimeSetupToken)
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
					// CEREBRO-PATCH(feature-flags-routes): FIR-2505 workspace-level
					// feature-flag override (owner/admin) — forces a flag on/off for
					// every member, optionally locked so members cannot override it.
					r.Put("/feature-flags/{key}/workspace", featureFlagsHandler.UpsertWorkspace)
					r.Delete("/feature-flags/{key}/workspace", featureFlagsHandler.DeleteWorkspace)
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
						// CEREBRO-PATCH(user-infisical-folders): admin allow-list of
						// Infisical folders this member may grant to their agents.
						r.Get("/infisical-folders", h.ListMemberInfisicalFolders)
						r.Put("/infisical-folders", h.ReplaceMemberInfisicalFolders)
					})
					r.Delete("/invitations/{invitationId}", h.RevokeInvitation)
					// CEREBRO-PATCH(cerebro-groups-routes): group create requires admin/owner (JEH-1172).
					r.Post("/groups", cerebroGroupsHandler.Create)
					// CEREBRO-PATCH(cerebro-roles-routes): FIR-2130 role create requires admin/owner.
					r.Post("/roles", cerebroRolesHandler.Create)
					// CEREBRO-PATCH(cerebro-grants-routes): JEH-1179 grant writes (admin/owner only).
					r.Post("/grants", cerebroGrantsHandler.Create)
					r.Patch("/grants/{grantId}", cerebroGrantsHandler.Update)
					r.Delete("/grants/{grantId}", cerebroGrantsHandler.Delete)
					// CEREBRO-PATCH(cerebro-tool-policy-routes): FIR-2230 per-tool policy writes (admin/owner only).
					r.Put("/tool-policy", cerebroToolPolicyHandler.Set)
					r.Delete("/tool-policy", cerebroToolPolicyHandler.Clear)
					// CEREBRO-PATCH(cerebro-cost-optimization-routes): FIR-2325 saving-mode writes (admin/owner only).
					r.Put("/cost-optimization/{key}", costOptimizationHandler.Upsert)
					r.Delete("/cost-optimization/{key}", costOptimizationHandler.Delete)
					// CEREBRO-PATCH(cerebro-cost-optimization-holdout): FIR-2640 per-saving holdout share writes (admin/owner only).
					r.Put("/cost-optimization/holdout/{key}", costOptimizationHandler.UpsertHoldout)
					r.Delete("/cost-optimization/holdout/{key}", costOptimizationHandler.DeleteHoldout)
					// CEREBRO-PATCH(cerebro-approvals-routes): FIR-2131 approval decisions + intake seam (admin/owner only).
					r.Post("/approvals/intake", cerebroApprovalsHandler.Intake)
					r.Post("/approvals/{approvalId}/approve", cerebroApprovalsHandler.Approve)
					r.Post("/approvals/{approvalId}/reject", cerebroApprovalsHandler.Reject)
					r.Post("/approvals/{approvalId}/delegate", cerebroApprovalsHandler.Delegate)
					// W4.6: audit feed for sandbox + admin actions.
					r.Get("/activity", h.ListWorkspaceActivity)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)

				// GitHub integration — connect / disconnect remain admin-only;
				// the read-only list endpoint lives in the member-level group
				// above so non-admins can see the workspace's connection state.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Get("/github/connect", h.GitHubConnect)
					r.Delete("/github/installations/{installationId}", h.DeleteGitHubInstallation)
				})
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
			r.Post("/current/renew", h.RenewCurrentPersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// Cloud Billing proxy. Same upstream service / port as
		// cloud-runtime — multica-cloud's Fleet and Billing share
		// :8080 and the same chi router. All routes here forward
		// to /api/v1/billing/* with X-User-ID stamped from the
		// authenticated context.
		//
		// User-scoped (account-level), NOT workspace-scoped — sits
		// outside the RequireWorkspaceMember group so a user can
		// inspect their balance, top up, and open the Billing Portal
		// without an active workspace selected. The upstream owner
		// model is single-user; X-Workspace-ID would be ignored even
		// if we sent it. The Stripe webhook is the public outlier
		// and lives outside the entire Auth group (see above).
		//
		// IMPORTANT — task-token actors are blocked here. The Auth
		// middleware happily turns an mat_ task token into a normal
		// X-User-ID stamp (so agents can comment, claim issues, etc.
		// as their owner), but billing is account-level and a running
		// agent reading its owner's balance / opening a checkout
		// session is the kind of lateral-movement we're explicitly
		// trying to prevent. handler.RequireHumanActor checks the
		// authoritative server-set X-Actor-Source header and 403s
		// any task-token request. See actor_guards.go for the full
		// rationale.
		r.Route("/api/cloud-billing", func(r chi.Router) {
			r.Use(handler.RequireHumanActor)

			r.Get("/balance", h.GetCloudBillingBalance)
			r.Get("/transactions", h.ListCloudBillingTransactions)
			r.Get("/batches", h.ListCloudBillingBatches)
			r.Get("/topups", h.ListCloudBillingTopups)
			r.Get("/price-tiers", h.ListCloudBillingPriceTiers)
			r.Post("/checkout-sessions", h.CreateCloudBillingCheckoutSession)
			r.Get("/checkout-sessions/{sessionId}", h.GetCloudBillingCheckoutSession)
			r.Post("/portal-sessions", h.CreateCloudBillingPortalSession)
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

			// CEREBRO-PATCH(cerebro-roles-routes): FIR-2130 role CRUD + assignment endpoints.
			r.Route("/api/roles", func(r chi.Router) {
				// Read-only: any workspace member.
				r.Get("/{id}", cerebroRolesHandler.Get)
				r.Get("/{id}/assignments", cerebroRolesHandler.ListAssignments)
				// Writes: admin/owner only.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
					r.Patch("/{id}", cerebroRolesHandler.Update)
					r.Delete("/{id}", cerebroRolesHandler.Delete)
					r.Post("/{id}/assignments", cerebroRolesHandler.Assign)
					r.Delete("/{id}/assignments/{subjectType}/{subjectId}", cerebroRolesHandler.Unassign)
				})
			})

			// CEREBRO-PATCH(cerebro-identity-routes): FIR-2523 Google Workspace identity-source settings endpoints.
			r.Route("/api/cerebro/workspaces/{id}/auth-settings", func(r chi.Router) {
				r.Get("/", cerebroIdentityHandler.Get)
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
					r.Put("/", cerebroIdentityHandler.Update)
				})
			})

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/search", h.SearchIssues)
				r.Get("/child-progress", h.ChildIssueProgress)
				r.Get("/children", h.ListChildrenByParents)
				r.Get("/grouped", h.ListGroupedIssues)
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
					// CEREBRO-PATCH(issue-dependencies): blocks/blocked-by/related relations (FIR-823).
					r.Get("/dependencies", h.ListIssueDependencies)
					r.Post("/blocks", h.AddBlocks)
					r.Delete("/blocks/{otherId}", h.DeleteBlocks)
					r.Post("/blocked-by", h.AddBlockedBy)
					r.Delete("/blocked-by/{otherId}", h.DeleteBlockedBy)
					r.Post("/related", h.AddRelated)
					r.Delete("/related/{otherId}", h.DeleteRelated)
					r.Get("/metadata", h.ListIssueMetadata)
					r.Put("/metadata/{key}", h.SetIssueMetadataKey)
					r.Delete("/metadata/{key}", h.DeleteIssueMetadataKey)
					r.Get("/pull-requests", h.ListPullRequestsForIssue)
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
					r.Put("/resources/{resourceId}", h.UpdateProjectResource)
					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
					r.Put("/parent", h.SetProjectParent)
					r.Put("/show-descendants", h.SetProjectShowDescendants)
					r.Get("/rollup-stats", h.GetProjectRollupStats)
				})
			})

			// Squads
			r.Route("/api/squads", func(r chi.Router) {
				r.Get("/", h.ListSquads)
				r.Post("/", h.CreateSquad)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSquad)
					r.Put("/", h.UpdateSquad)
					r.Delete("/", h.DeleteSquad)
					r.Get("/members", h.ListSquadMembers)
					r.Get("/members/status", h.ListSquadMemberStatus)
					r.Post("/members", h.AddSquadMember)
					r.Delete("/members", h.RemoveSquadMember)
					r.Patch("/members/role", h.UpdateSquadMemberRole)
				})
			})

			// Squad leader evaluation (writes to activity_log)
			r.Post("/api/issues/{id}/squad-evaluated", h.RecordSquadLeaderEvaluation)

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
					r.Get("/runs/{runId}", h.GetAutopilotRun)
					r.Get("/deliveries", h.ListAutopilotDeliveries)
					r.Get("/deliveries/{deliveryId}", h.GetAutopilotDelivery)
					r.Post("/deliveries/{deliveryId}/replay", h.ReplayAutopilotDelivery)
					r.Post("/triggers", h.CreateAutopilotTrigger)
					r.Route("/triggers/{triggerId}", func(r chi.Router) {
						r.Patch("/", h.UpdateAutopilotTrigger)
						r.Delete("/", h.DeleteAutopilotTrigger)
						r.Post("/rotate-webhook-token", h.RotateAutopilotTriggerWebhookToken)
						r.Put("/signing-secret", h.SetAutopilotTriggerSigningSecret)
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
			r.Get("/api/attachments/{id}/content", h.GetAttachmentContent)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			// Comments
			// CEREBRO-PATCH(comments-move-to-thread): JEH-2488 multi-select → new thread.
			r.Post("/api/comments/move-to-thread", cerebroCommentsHandler.MoveToThread)
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/resolve", h.ResolveComment)
				r.Delete("/resolve", h.UnresolveComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
				// CEREBRO-PATCH(comments-move-to-subissue): JEH-1309 thread → sub-issue.
				r.Post("/move-to-subissue", cerebroCommentsHandler.MoveToSubIssue)
			})

			// Agents
			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)
				// Agent templates: pre-configured instructions + skill refs.
				// Picking a template imports the referenced skills into the
				// workspace (find-or-create by name) and creates the agent
				// with the template's instructions in one transaction.
				r.Post("/from-template", h.CreateAgentFromTemplate)
				// CEREBRO-PATCH(agent-avatar-generate): JEH-1563 AI avatar generation endpoint
				r.Post("/generate-avatar", cerebroAgentAvatarHandler.Generate)
				r.Post("/backfill-avatars", cerebroAgentAvatarHandler.Backfill) // CEREBRO-PATCH(agent-avatar-backfill): async workspace avatar backfill.
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
					// CEREBRO-PATCH(agent-tool-overrides-routes): JEH-1710 per-agent
					// override of the runtime tool default.
					r.Get("/tool-overrides", h.ListAgentToolOverrides)
					r.Put("/tool-overrides/{toolName}", h.PutAgentToolOverride)
					r.Delete("/tool-overrides/{toolName}", h.DeleteAgentToolOverride)
					// CEREBRO-PATCH(agent-infisical-secrets): Infisical folder grants for daemon spawn injection.
					r.Get("/infisical-folders", h.ListAgentInfisicalFolders)
					r.Put("/infisical-folders", h.ReplaceAgentInfisicalFolders)
					// Folders the agent owner is allowed to grant — drives the picker.
					r.Get("/infisical-allowed-folders", h.ListAgentAllowedInfisicalFolders)
					// Dedicated env-management endpoint. Owner/admin only;
					// agent actors are denied. Every reveal / write is
					// audited to activity_log. See MUL-2600 and
					// internal/handler/agent_env.go.
					r.Get("/env", h.GetAgentEnv)
					r.Put("/env", h.UpdateAgentEnv)
				})
			})

			// Agent templates catalog (browse + detail). The Create flow
			// lives under /api/agents/from-template above; this route is for
			// the picker UI to list available templates.
			r.Route("/api/agent-templates", func(r chi.Router) {
				r.Get("/", h.ListAgentTemplates)
				r.Get("/{slug}", h.GetAgentTemplate)
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
					// CEREBRO-PATCH(skill-fork-parent-lineage): FIR-2629 — "forked from" lineage for the web UI.
					r.Get("/fork-parent", h.GetSkillForkParent)
				})
			})

			// Usage
			r.Route("/api/usage", func(r chi.Router) {
				r.Get("/daily", h.GetWorkspaceUsageByDay)
				r.Get("/summary", h.GetWorkspaceUsageSummary)
			})

			// Dashboard — workspace-wide token + run-time rollups for the
			// "/{slug}/dashboard" page. Optional ?project_id filter scopes
			// the rollup to a single project.
			r.Route("/api/dashboard", func(r chi.Router) {
				r.Get("/usage/daily", h.GetDashboardUsageDaily)
				r.Get("/usage/by-agent", h.GetDashboardUsageByAgent)
				r.Get("/agent-runtime", h.GetDashboardAgentRunTime)
				r.Get("/runtime/daily", h.GetDashboardRunTimeDaily)
			})

			// CEREBRO-PATCH(router-capability-register): FIR-2129 normalized capability registry.
			r.Route("/api/capabilities", func(r chi.Router) {
				r.Get("/", h.ListCapabilities)
				r.Post("/report", h.ReportCapabilities)
			})

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Route("/{runtimeId}", func(r chi.Router) {
					r.Patch("/", h.UpdateAgentRuntime)
					r.Get("/usage", h.GetRuntimeUsage)
					r.Get("/usage/by-agent", h.GetRuntimeUsageByAgent)
					r.Get("/usage/by-hour", h.GetRuntimeUsageByHour)
					r.Get("/activity", h.GetRuntimeTaskActivity)
					r.Post("/update", h.InitiateUpdate)
					r.Get("/update/{updateId}", h.GetUpdate)
					// CEREBRO-PATCH(runtime-sandbox): cerebro daemon sandbox toggle endpoint.
					r.Patch("/sandbox", h.UpdateAgentRuntimeSandbox)
					r.Patch("/sandbox-policy", h.UpdateAgentRuntimeSandboxPolicy)
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
					// CEREBRO-PATCH(router-runtime-tools-config): runtime-level MCP defaults (9031).
					r.Patch("/tools-config", h.UpdateAgentRuntimeToolsConfig)
					// CEREBRO-PATCH(router-runtime-tools-admin): JEH-1710 unified
					// runtime tool inventory + per-tool group/user access grants.
					r.Get("/tools", h.ListRuntimeTools)
					r.Patch("/tools/{toolName}", h.SetRuntimeToolEnabled)
					// CEREBRO-PATCH(router-runtime-tools-scan-now): FIR-2230 admin-triggered live scan.
					r.Post("/tools/scan-now", h.RequestRuntimeToolScan)
					r.Get("/tool-grants", h.ListRuntimeToolGrants)
					r.Post("/tools/{toolName}/groups/{groupId}", h.AddRuntimeToolGroupGrant)
					r.Delete("/tools/{toolName}/groups/{groupId}", h.RemoveRuntimeToolGroupGrant)
					r.Post("/tools/{toolName}/users/{userId}", h.AddRuntimeToolUserGrant)
					r.Delete("/tools/{toolName}/users/{userId}", h.RemoveRuntimeToolUserGrant)
					r.Delete("/", h.DeleteAgentRuntime)
					// Cascade variant of DELETE: archive every active agent
					// bound to this runtime, cancel their tasks, then delete
					// the runtime — all in one transaction. Used by the
					// DeleteRuntimeDialog when the strict DELETE refused with
					// `runtime_has_active_agents` and the user confirmed the
					// cascade plan.
					r.Post("/archive-agents-and-delete", h.ArchiveAgentsAndDeleteRuntime)
				})
			})

			// Cloud Runtime fleet proxy. The remote service URL is configured
			// on SaaS API nodes only; self-hosted deployments return 503.
			r.Route("/api/cloud-runtime", func(r chi.Router) {
				r.Get("/", h.GetCloudRuntimeService)
				r.Get("/healthz", h.GetCloudRuntimeHealth)
				r.Get("/readyz", h.GetCloudRuntimeReady)
				r.Get("/nodes", h.ListCloudRuntimeNodes)
				r.Post("/nodes", h.CreateCloudRuntimeNode)
				r.Delete("/nodes", h.DeleteCloudRuntimeNode)
				r.Post("/nodes/start", h.StartCloudRuntimeNode)
				r.Post("/nodes/stop", h.StopCloudRuntimeNode)
				r.Post("/nodes/reboot", h.RebootCloudRuntimeNode)
				r.Post("/nodes/status", h.GetCloudRuntimeNodeStatus)
				r.Post("/nodes/exec", h.ExecCloudRuntimeNode)
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
					r.Patch("/", h.UpdateChatSession)
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
				r.Get("/active-issue-tasks", cerebroInboxHandler.ListActiveIssueTasks)
				r.Post("/reminders", cerebroInboxHandler.CreateReminder) // CEREBRO-PATCH(inbox-reminders-route): create muted reminder items.
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
				// CEREBRO-PATCH(cerebro-notifications-routes): FIR-2394 — wire the
				// route='notifications' inbox slice that the frontend has been
				// calling all along; previously these returned 404 in prod.
				r.Get("/notifications", cerebroInboxHandler.ListNotifications)
				r.Get("/notifications/unread-count", cerebroInboxHandler.CountUnreadNotifications)
				r.Post("/notifications/mark-all-read", cerebroInboxHandler.MarkAllNotificationsRead)
				r.Post("/notifications/archive-all", cerebroInboxHandler.ArchiveAllNotifications)
				// CEREBRO-PATCH(cerebro-inbox-routes): FIR-2385 — owner accepts a private-agent run-request.
				r.Post("/{id}/run-private-agent", cerebroInboxHandler.RunPrivateAgentRequest)

			})

			// Notification preferences
			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreferences)
				r.Put("/", h.UpdateNotificationPreferences)
			})

			// CEREBRO-PATCH(cerebro-dashboard-route): JEH-684 dashboard overview endpoint
			r.Get("/api/cerebro/dashboard", cerebroDashboardHandler.Overview)
			// CEREBRO-PATCH(cerebro-duplicate-check-route): FIR-2504 "find similar at create" endpoint + adoption-event sink.
			r.Post("/api/cerebro/issues/check-similar", h.CheckSimilarIssues)
			r.Post("/api/cerebro/issues/check-similar/event", h.DupCheckEvent)
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
			r.Mount("/api/cerebro/agent-passes", cerebroagentpass.NewAdminRoutes(cerebroQueries)) // CEREBRO-PATCH(cerebro-agent-passes-routes): JEH-1731 agent-pass admin API.
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
			// CEREBRO-PATCH(cerebro-sprints-routes): FIR-2666 project sprint REST surface.
			r.Route("/api/cerebro/projects/{projectID}/sprint-settings", func(r chi.Router) {
				r.Get("/", cerebroSprintsHandler.GetSettings)
				r.Put("/", cerebroSprintsHandler.UpsertSettings)
				r.Delete("/", cerebroSprintsHandler.DeleteSettings)
			})
			r.Route("/api/cerebro/projects/{projectID}/sprints", func(r chi.Router) {
				r.Get("/", cerebroSprintsHandler.ListSprints)
				r.Post("/", cerebroSprintsHandler.CreateSprint)
			})
			r.Route("/api/cerebro/projects/{projectID}/sprint-recurring-tasks", func(r chi.Router) {
				r.Get("/", cerebroSprintsHandler.ListRecurringTasks)
				r.Post("/", cerebroSprintsHandler.CreateRecurringTask)
			})
			r.Route("/api/cerebro/sprint-recurring-tasks/{id}", func(r chi.Router) {
				r.Put("/", cerebroSprintsHandler.UpdateRecurringTask)
				r.Delete("/", cerebroSprintsHandler.DeleteRecurringTask)
			})
			r.Route("/api/cerebro/sprints/{sprintID}", func(r chi.Router) {
				r.Get("/", cerebroSprintsHandler.GetSprint)
				r.Put("/", cerebroSprintsHandler.UpdateSprint)
				r.Delete("/", cerebroSprintsHandler.DeleteSprint)
				r.Get("/issues", cerebroSprintsHandler.ListSprintIssues)
			})
			r.Route("/api/cerebro/issues/{issueID}/sprint", func(r chi.Router) {
				r.Get("/", cerebroSprintsHandler.GetIssueAssignment)
				r.Put("/", cerebroSprintsHandler.AssignIssue)
			})
			// CEREBRO-PATCH(cerebro-status-models-routes): FIR-1550 workflow v2a status-model REST surface.
			r.Route("/api/cerebro/status-models", func(r chi.Router) {
				// Read-only: any workspace member (the board needs to resolve labels).
				r.Get("/", cerebroStatusModelsHandler.List)
				r.Get("/assignments", cerebroStatusModelsHandler.Assignments)
				r.Get("/{id}", cerebroStatusModelsHandler.Get)
				// Writes: admin/owner only (FIR-1550 — only workspace-admin controls models).
				r.Group(func(r chi.Router) {
					// CEREBRO-PATCH(cerebro-status-models-admin-gate): FIR-1550 model writes require admin/owner.
					r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
					r.Post("/", cerebroStatusModelsHandler.Create)
					r.Put("/{id}", cerebroStatusModelsHandler.Update)
					r.Delete("/{id}", cerebroStatusModelsHandler.Delete)
				})
			})
			// CEREBRO-PATCH(cerebro-status-models-routes): FIR-1550 per-project status-model selection.
			r.Route("/api/cerebro/projects/{projectId}/status-model", func(r chi.Router) {
				// Read-only: any workspace member.
				r.Get("/", cerebroStatusModelsHandler.GetProjectModel)
				// Writes: admin/owner only (FIR-1550 — only workspace-admin controls project selection).
				r.Group(func(r chi.Router) {
					// CEREBRO-PATCH(cerebro-status-models-admin-gate): FIR-1550 project-model writes require admin/owner.
					r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
					r.Put("/", cerebroStatusModelsHandler.SetProjectModel)
					r.Delete("/", cerebroStatusModelsHandler.ClearProjectModel)
				})
			})
			// CEREBRO-PATCH(cerebro-status-models-routes): FIR-1550 v2b per-issue custom-status pin.
			r.Route("/api/cerebro/issues/{issueId}/custom-status", func(r chi.Router) {
				r.Get("/", cerebroStatusModelsHandler.GetIssueCustomStatus)
				r.Put("/", cerebroStatusModelsHandler.SetIssueCustomStatus)
				r.Delete("/", cerebroStatusModelsHandler.ClearIssueCustomStatus)
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

// optionalUUID returns a NULL pgtype.UUID for an empty string and otherwise
// behaves like parseUUID. Use this for actor IDs on events where the producer
// may legitimately be a "system" actor with no member/agent attribution
// (e.g. GitHub webhook auto-status sync) — the activity_log and inbox_item
// tables both allow actor_id to be NULL.
func optionalUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
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

func cloudRuntimeFleetURLFromEnv() string {
	if url := strings.TrimSpace(os.Getenv("MULTICA_CLOUD_FLEET_URL")); url != "" {
		return url
	}
	return strings.TrimSpace(os.Getenv("MULTICA_FLEET_URL"))
}
