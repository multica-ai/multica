package main

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	composiointeg "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	composiosdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/llm"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

var defaultOrigins = []string{
	"http://localhost:3000", // Next.js dev
	"http://localhost:5173", // electron-vite dev
	"http://localhost:5174", // electron-vite dev (fallback port)
}

const pluginBridgePrefix = "/api/plugin-bridge/v1"

// corsAllowedHeaders must list every header the browser clients send. A header
// missing here fails the preflight, so the request never reaches the handler at
// all — the failure looks nothing like "the server ignored my header".
// X-Client-Capabilities in particular was daemon-only (a Go client, never
// preflighted) until the web app started advertising chat-draft-restore-v1 on
// cancel.
var corsAllowedHeaders = []string{
	"Accept",
	"Authorization",
	"Content-Type",
	"Idempotency-Key",
	"If-Match",
	"X-Workspace-ID",
	"X-Workspace-Slug",
	"X-Request-ID",
	"X-Agent-ID",
	"X-Task-ID",
	"X-CSRF-Token",
	"X-Client-Platform",
	"X-Client-Version",
	"X-Client-OS",
	"X-Client-Capabilities",
	// Sent by the host page when it relays a plugin surface's Action API call.
	"X-Multica-Plugin-Installation",
}

// corsExposedHeaders lists response headers browser clients are allowed to read.
// Without this a custom response header is silently unreadable from JS on a
// cross-origin request (only the CORS-safelisted response headers are exposed by
// default) — the header arrives on the wire and then disappears, which looks
// exactly like the server never sent it.
//
// Referencing the handler constant rather than re-typing the string keeps a
// rename from quietly switching the signal off (MUL-5492).
var corsExposedHeaders = []string{
	"ETag",
	"X-Request-ID",
	handler.HeaderCommentsTruncated,
	handler.HeaderTimelineTruncated,
}

func registerPluginActionRoutes(r chi.Router, h *handler.Handler) {
	r.Get(publicapiv1.PathContext, h.GetPluginContext)
	r.Get(publicapiv1.PathIssue, h.GetPluginIssue)
	r.Patch(publicapiv1.PathIssue, h.PatchPluginIssue)
	r.Get(publicapiv1.PathIssueComments, h.ListPluginComments)
	r.Post(publicapiv1.PathIssueComments, h.CreatePluginComment)
	r.Get(publicapiv1.PathStorageScope, h.ListPluginStorage)
	r.Get(publicapiv1.PathStorageValue, h.GetPluginStorage)
	r.Put(publicapiv1.PathStorageValue, h.PutPluginStorage)
	r.Delete(publicapiv1.PathStorageValue, h.DeletePluginStorage)
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

// appURLFromEnv resolves the user-facing web app URL. It prefers
// MULTICA_APP_URL and falls back to FRONTEND_ORIGIN, matching how the backend
// resolves the app URL elsewhere (handler.daemonSetupURLsFromEnv) and the CLI
// login flow (cmd/multica tryResolveAppURL). Empty when neither is set.
func appURLFromEnv() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_APP_URL")), "/"); v != "" {
		return v
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")), "/")
}

// pluginActionBaseURL resolves the versioned public base a hook handler calls
// back into. Managed deployments give the Plugin API its own hostname through
// MULTICA_PLUGIN_API_URL; self-hosted and local deployments can leave it empty
// and serve the same /v1 contract on MULTICA_PUBLIC_URL.
func pluginActionBaseURL(publicURL string) string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PLUGIN_API_URL")), "/"); value != "" {
		return value
	}
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" {
		return ""
	}
	return publicURL + publicapiv1.BasePath
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

// normalizeServerVersion maps the unstamped "dev" default (main.go's
// `version` var, unchanged when the binary wasn't built with
// -X main.version=<tag>) to an empty string. handler.Config.ServerVersion
// feeds /api/config's server_version field with omitempty, so an empty
// string hides the Help popover's version row instead of rendering
// "Server version dev" for a local `go build`/`go run` or a self-hosted
// `docker build` without --build-arg VERSION.
func normalizeServerVersion(v string) string {
	if v == "dev" {
		return ""
	}
	return v
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
// rdb is optional: when non-nil the runtime local-skill request stores are
// swapped for Redis-backed implementations so multiple API nodes share the
// same pending queue (required for multi-node prod). This should be a request
// path Redis client, not the realtime relay's blocking read client. A nil rdb
// keeps the default in-memory stores which are fine for single-node dev and
// tests.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client) chi.Router {
	r, _ := NewRouterWithOptions(pool, hub, bus, analyticsClient, rdb, RouterOptions{})
	return r
}

type RouterOptions struct {
	HTTPMetrics         *obsmetrics.HTTPMetrics
	BusinessMetrics     *obsmetrics.BusinessMetrics
	ChannelLeaseMetrics *obsmetrics.ChannelLeaseMetrics
	SeatCapacityMetrics *obsmetrics.SeatCapacityMetrics
	// ChannelLeaseRedis is a dedicated non-blocking Redis client/pool. It is
	// required only when CHANNEL_WS_LEASE_BACKEND=redis.
	ChannelLeaseRedis *redis.Client
	// WecomMetrics is the WeCom adapter's health sink. Nil discards every
	// counter, which is what a deployment with /metrics turned off gets.
	WecomMetrics *obsmetrics.WecomMetrics
	DaemonHub    *daemonws.Hub
	DaemonWakeup service.TaskWakeupNotifier
	FeatureFlags *featureflag.Service
	// HeartbeatScheduler, when non-nil, replaces the default synchronous
	// passthrough scheduler on the constructed Handler. main.go injects a
	// BatchedHeartbeatScheduler here so the caller can also drive Run/Stop;
	// tests leave this nil and get the legacy synchronous behavior.
	HeartbeatScheduler handler.HeartbeatScheduler
	// LLMMaxRetries carries the parsed MULTICA_LLM_MAX_RETRIES budget. Unlike
	// its three MULTICA_LLM_* siblings it is injected rather than read here,
	// because an invalid value must fail the boot and only main() can exit —
	// terminating the process from inside a router constructor would also kill
	// any test that happened to have the variable set. nil means unset, which
	// is what tests and NewRouter get.
	LLMMaxRetries *llm.RetryOverride
}

func buildChannelSupervisor(
	installations engine.InstallationStore,
	postgresLeases engine.LeaseStore,
	registry *channel.Registry,
	inbound channel.InboundHandler,
	opts RouterOptions,
) *engine.Supervisor {
	cfg, err := channelSupervisorConfigFromEnv(opts.ChannelLeaseMetrics)
	if err != nil {
		slog.Error("channel engine: invalid lease configuration; supervisor disabled", "error", err)
		return nil
	}

	backend := strings.ToLower(strings.TrimSpace(os.Getenv("CHANNEL_WS_LEASE_BACKEND")))
	if backend == "" {
		backend = "postgres"
	}
	var leases engine.LeaseStore
	switch backend {
	case "postgres":
		leases = postgresLeases
	case "redis":
		if opts.ChannelLeaseRedis == nil {
			slog.Error("channel engine: Redis lease backend selected but CHANNEL_WS_LEASE_REDIS_URL/REDIS_URL is missing or invalid; supervisor disabled")
			return nil
		}
		namespace := strings.TrimSpace(os.Getenv("CHANNEL_WS_LEASE_NAMESPACE"))
		redisLeases, err := engine.NewRedisLeaseStore(opts.ChannelLeaseRedis, namespace)
		if err != nil {
			slog.Error("channel engine: Redis lease configuration invalid; supervisor disabled", "error", err)
			return nil
		}
		readyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = redisLeases.Ready(readyCtx)
		cancel()
		if err != nil {
			slog.Error("channel engine: Redis lease backend unavailable; supervisor disabled", "error", err)
			return nil
		}
		leases = redisLeases
	default:
		slog.Error("channel engine: unsupported CHANNEL_WS_LEASE_BACKEND; supervisor disabled", "backend", backend)
		return nil
	}

	slog.Info("channel engine: lease backend configured",
		"backend", backend,
		"ttl", cfg.LeaseTTL.String(),
		"renew_interval", cfg.LeaseRenewInterval.String(),
		"poll_interval", cfg.PollInterval.String(),
	)
	return engine.NewSupervisor(installations, leases, registry, inbound, cfg)
}

func channelSupervisorConfigFromEnv(leaseMetrics *obsmetrics.ChannelLeaseMetrics) (engine.Config, error) {
	ttl, err := strictPositiveDurationEnv("CHANNEL_WS_LEASE_TTL", 180*time.Second)
	if err != nil {
		return engine.Config{}, err
	}
	renew, err := strictPositiveDurationEnv("CHANNEL_WS_LEASE_RENEW_INTERVAL", 60*time.Second)
	if err != nil {
		return engine.Config{}, err
	}
	poll, err := strictPositiveDurationEnv("CHANNEL_WS_LEASE_POLL_INTERVAL", 30*time.Second)
	if err != nil {
		return engine.Config{}, err
	}
	retry, err := strictPositiveDurationEnv("CHANNEL_WS_LEASE_ERROR_RETRY_INTERVAL", 5*time.Second)
	if err != nil {
		return engine.Config{}, err
	}
	margin, err := strictPositiveDurationEnv("CHANNEL_WS_LEASE_EXPIRY_SAFETY_MARGIN", 5*time.Second)
	if err != nil {
		return engine.Config{}, err
	}
	cfg := engine.Config{
		LeaseTTL:                ttl,
		LeaseRenewInterval:      renew,
		PollInterval:            poll,
		LeaseErrorRetryInterval: retry,
		LeaseExpirySafetyMargin: margin,
		LeaseMetrics:            leaseMetrics,
		Logger:                  slog.Default(),
	}
	if err := cfg.Validate(); err != nil {
		return engine.Config{}, err
	}
	return cfg, nil
}

func strictPositiveDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration (got %q)", name, raw)
	}
	return value, nil
}

func seatCapacityExecutor(cloudURL string) seatcapacity.Executor {
	executor, err := seatcapacity.New(seatcapacity.Config{
		BaseURL: cloudURL,
	})
	if err == nil {
		return executor
	}
	// A Cloud-connected deployment with malformed connection settings must not
	// silently restore unlimited membership. The fail-closed executor returns
	// 503 until the operator repairs the Cloud URL; it is deliberately
	// ineligible for recovery-worker startup so queued intents keep their retry
	// budget while configuration is broken.
	slog.Error("subscription seat capacity executor unavailable", "error", err)
	return seatcapacity.NewUnavailable(err)
}

// NewRouterWithOptions builds the fully-configured Chi router and
// returns the *handler.Handler it was constructed from. Callers that
// need to drive background lifecycle on services attached to the
// handler (e.g. starting the Lark inbound Hub under a long-running
// context, calling Wait on shutdown) use the returned handler;
// callers that only need the HTTP handler (tests, the simple
// NewRouter shim) discard the second value.
func NewRouterWithOptions(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client, opts RouterOptions) (chi.Router, *handler.Handler) {
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
	origins := allowedOrigins()

	signupConfig := handler.Config{
		AllowSignup:              os.Getenv("ALLOW_SIGNUP") != "false",
		AllowedEmails:            splitAndTrim(os.Getenv("ALLOWED_EMAILS")),
		AllowedEmailDomains:      splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS")),
		DisableWorkspaceCreation: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
		VCSIntegrationEnabled:    os.Getenv("MULTICA_VCS_INTEGRATION_ENABLED") == "true",
		PublicURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PUBLIC_URL")), "/"),
		AppURL:                   appURLFromEnv(),
		TrustedProxies:           parseTrustedProxies(os.Getenv("MULTICA_TRUSTED_PROXIES")),
		CloudURL:                 strings.TrimSpace(os.Getenv("MULTICA_CLOUD_URL")),
		CloudTimeout:             35 * time.Second,
		AttachmentDownloadMode:   os.Getenv("ATTACHMENT_DOWNLOAD_MODE"),
		AttachmentDownloadURLTTL: envDuration("ATTACHMENT_DOWNLOAD_URL_TTL", 30*time.Minute),
		AttachmentFrameAncestors: origins,
		PluginSurfaceOrigin:      strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PLUGIN_SURFACE_ORIGIN")), "/"),
		LLMAPIKey:                strings.TrimSpace(os.Getenv("MULTICA_LLM_API_KEY")),
		LLMBaseURL:               strings.TrimSpace(os.Getenv("MULTICA_LLM_BASE_URL")),
		LLMDefaultModel:          strings.TrimSpace(os.Getenv("MULTICA_LLM_DEFAULT_MODEL")),
		LLMMaxRetries:            opts.LLMMaxRetries,
		ServerVersion:            normalizeServerVersion(version),
	}
	h := handler.New(queries, pool, hub, bus, emailSvc, store, cfSigner, analyticsClient, signupConfig, daemonHub)
	invitationRateLimits := handler.DefaultInvitationRateLimits()
	invitationRateLimits.Actor.Limit = envNonNegativeInt("RATE_LIMIT_INVITATION_ACTOR_10M", invitationRateLimits.Actor.Limit)
	invitationRateLimits.Workspace.Limit = envNonNegativeInt("RATE_LIMIT_INVITATION_WORKSPACE_24H", invitationRateLimits.Workspace.Limit)
	invitationRateLimits.Recipient.Limit = envNonNegativeInt("RATE_LIMIT_INVITATION_RECIPIENT_24H", invitationRateLimits.Recipient.Limit)
	h.InvitationRateLimiters = handler.NewMemoryInvitationRateLimiters(invitationRateLimits)
	h.Metrics = opts.BusinessMetrics
	h.FeatureFlags = opts.FeatureFlags
	h.TaskService.FeatureFlags = opts.FeatureFlags
	h.TaskService.Metrics = opts.BusinessMetrics
	h.IssueService.Metrics = opts.BusinessMetrics
	entitlementClient, entitlementErr := entitlement.New(entitlement.Config{
		BaseURL:  signupConfig.CloudURL,
		Observer: opts.BusinessMetrics,
	})
	if entitlementErr != nil {
		slog.Error("entitlement policy client disabled by malformed Multica Cloud URL", "error", entitlementErr)
		opts.BusinessMetrics.RecordEntitlementConfigError()
	} else if entitlementClient.Enabled() {
		h.Entitlements = entitlementClient
		h.AutopilotService.Entitlements = entitlementClient
		h.AutopilotService.QuotaMetrics = opts.BusinessMetrics
	}
	// Cloud Runtime and strict seat capacity are one managed deployment. Reuse
	// the same base URL so Billing cannot be readable while invitation writes
	// silently skip Cloud's authoritative seat policy.
	h.SeatCapacity = seatCapacityExecutor(signupConfig.CloudURL)
	capacityLocker := seatcapacity.NewWorkspaceLocker(pool)
	h.SeatCapacityLocker = capacityLocker
	if seatcapacity.CanRunWorker(h.SeatCapacity) {
		h.SeatCapacityWorker = seatcapacity.NewWorker(queries, h.SeatCapacity, capacityLocker, seatcapacity.WorkerConfig{
			Metrics: opts.SeatCapacityMetrics,
		})
	}
	if opts.BusinessMetrics != nil {
		// Wire the BusinessMetrics receiver into the cloud runtime client
		// so every outbound Fleet/Gateway request feeds the
		// multica_cloudruntime_request_* histograms.
		if client, ok := h.CloudRuntime.(*cloudruntime.Client); ok {
			client.SetRecorder(opts.BusinessMetrics)
		}
	}
	if opts.DaemonWakeup != nil {
		h.TaskService.Wakeup = opts.DaemonWakeup
		if notifier, ok := opts.DaemonWakeup.(handler.RuntimeProfileRefreshNotifier); ok {
			h.DaemonProfileRefresh = notifier
		}
		if notifier, ok := opts.DaemonWakeup.(handler.WorkspaceSetRefreshNotifier); ok {
			h.DaemonWorkspaceRefresh = notifier
		}
		if notifier, ok := opts.DaemonWakeup.(handler.DaemonPendingWorkNotifier); ok {
			h.DaemonPendingWork = notifier
		}
	}
	if rdb != nil {
		h.UpdateStore = handler.NewRedisUpdateStore(rdb)
		h.ModelListStore = handler.NewRedisModelListStore(rdb)
		h.ModelCatalogCache = handler.NewRedisModelCatalogCache(rdb)
		h.LocalSkillListStore = handler.NewRedisLocalSkillListStore(rdb)
		h.LocalSkillImportStore = handler.NewRedisLocalSkillImportStore(rdb)
		h.LivenessStore = handler.NewRedisLivenessStore(rdb)
		h.WebhookRateLimiter = handler.NewRedisWebhookRateLimiter(rdb, handler.DefaultWebhookRateLimit())
		h.WebhookIPRateLimiter = handler.NewRedisWebhookIPRateLimiter(rdb, handler.DefaultWebhookIPRateLimit())
		h.WebhookAbsoluteIPRateLimiter = handler.NewRedisWebhookAbsoluteIPRateLimiter(rdb, handler.DefaultWebhookAbsoluteIPRateLimit())
		h.InvitationRateLimiters = handler.NewRedisInvitationRateLimiters(rdb, invitationRateLimits)
	}

	// Channel engine (MUL-3620): the platform-agnostic inbound runtime.
	// Built UNCONDITIONALLY — it drives any channel.Channel, not just
	// Feishu, so it must not depend on the Lark master key (a future
	// Slack-only deployment has no Lark key). Platform adapters register a
	// Factory + ResolverSet into it below; the Supervisor enumerates active
	// installations across ALL channel types and routes each to its
	// registered platform's Factory. Installations whose channel_type has no
	// registered Factory are skipped by the Supervisor — either no platform is
	// configured, or (Slack/B2) the platform drives ONE deployment-level
	// connection of its own outside the per-installation supervisor. The Router
	// is the single shared inbound handler injected into every Channel.
	channelRegistry := channel.NewRegistry()
	channelRouter := engine.NewRouter(h.IssueService, h.TaskService, queries, engine.RouterConfig{
		Logger: slog.Default(), Lifecycle: h,
	})
	// Debounce the per-session run trigger so a burst of messages collapses
	// into one agent run instead of one per message (MUL-2968).
	channelRouter.EnableRunBatching(engine.DefaultChatRunBatchWindow)
	h.ChannelRouter = channelRouter
	// Media intent-ledger reconciler: settles uploaded-but-unbound objects.
	// Built ONLY when a storage backend exists — store is nil when S3 is not
	// configured and the local upload dir failed to initialize, and a
	// reconciler with nil Storage would panic the worker goroutine on the
	// first unreferenced row (ledger rows can pre-exist from a boot where
	// storage WAS configured). Without storage the resolver skips every
	// upload, so no new rows appear and the ledger simply waits for a boot
	// with working storage. Started from main.go as its own worker.
	if store != nil {
		h.ChannelMediaReconciler = &service.ChannelMediaReconciler{
			Queries: queries,
			Storage: store,
			Logger:  slog.Default(),
		}
	}
	installationStore := lark.NewChannelInstallationStore(queries)
	h.ChannelSupervisor = buildChannelSupervisor(
		installationStore,
		installationStore,
		channelRegistry,
		channelRouter.Handle,
		opts,
	)

	// Lark integration. Only wired when MULTICA_LARK_SECRET_KEY is set:
	// the InstallationService refuses to fall back to plaintext storage
	// for app_secret, and the BindingTokenService cannot mint usable
	// tokens without it either. When the key is absent the Lark
	// handlers return 503 with a clear message; the rest of the server
	// continues to start so self-host deployments that have not opted
	// in to Lark are unaffected. Feishu registers its Factory + ResolverSet
	// into the channel engine above.
	if larkKey, err := secretbox.LoadKey("MULTICA_LARK_SECRET_KEY"); err == nil {
		box, err := secretbox.New(larkKey)
		if err != nil {
			slog.Error("lark: secretbox.New failed; lark integration disabled", "error", err)
		} else {
			installSvc, err := lark.NewInstallationService(queries, box)
			if err != nil {
				slog.Error("lark: InstallationService init failed; lark integration disabled", "error", err)
			} else {
				h.LarkInstallations = installSvc
				h.LarkBindingTokens = lark.NewBindingTokenService(queries, pool)
				slog.Info("lark integration enabled")

				// APIClient: wire the real Lark Open Platform HTTP client
				// (IM v1 send/patch + binding-prompt + bot info). Setting
				// MULTICA_LARK_SECRET_KEY is the operator's opt-in for
				// the integration as a whole; we don't expose a separate
				// "HTTP enabled" knob because the inbound dispatcher
				// without outbound replies is not a useful production
				// state, and CI / integration tests that want to avoid
				// real Lark traffic can point MULTICA_LARK_HTTP_BASE_URL
				// at a mock server.
				//
				// MULTICA_LARK_HTTP_BASE_URL is an OPTIONAL deployment-wide
				// override. Normal operation leaves it empty: each call then
				// resolves its open-platform host from the installation's
				// region (open.feishu.cn vs open.larksuite.com), so one
				// deployment serves both clouds. Set it only to force every
				// installation onto one host — a proxy, a mock for tests, or
				// a single-cloud staging setup.
				larkClient := lark.NewHTTPAPIClient(lark.HTTPClientConfig{
					BaseURL: strings.TrimSpace(os.Getenv("MULTICA_LARK_HTTP_BASE_URL")),
					Logger:  slog.Default(),
				})
				h.LarkAPIClient = larkClient

				// Channel-backed store: routes the lark package's DB seams
				// onto the channel_* tables (MUL-3515). Interface-wired
				// consumers (patcher, typing indicator, dispatcher, hub,
				// backfills) take it directly; the constructor-based services
				// wrap *db.Queries internally, so they keep taking queries.
				cs := lark.NewChannelStore(queries)
				patcher := lark.NewPatcher(cs, installSvc, larkClient, lark.PatcherConfig{})
				patcher.Register(bus)

				// Typing indicator: shows a "processing" reaction on the user's
				// message while the agent is working, then removes it before the
				// reply is sent. Best-effort; failures are logged only.
				typingIndicator := lark.NewTypingIndicatorManager(larkClient, installSvc, cs, slog.Default())
				patcher.SetTypingIndicatorManager(typingIndicator)

				// Inbound pipeline seams: lark_inbound_audit logger and the
				// shared channel-agnostic chat-session service. They back the
				// Feishu ResolverSet that the engine.Router runs through,
				// sharing the same IssueService + TaskService that back HTTP, so
				// /issue-created issues share counter, dup guard, project
				// boundary, broadcast, analytics and agent-enqueue with the rest
				// of the product. Feishu is just another consumer of the shared
				// engine.ChatSession (channel_type-keyed); the Lark session
				// titles preserve the pre-cutover wording.
				auditLogger := lark.NewAuditLogger(queries)
				feishuSession := engine.NewChatSession(queries, pool, channel.TypeFeishu, engine.SessionTitles{
					Group:    "Lark group chat",
					Direct:   "Lark direct message",
					Fallback: "Lark chat",
				})

				// OutcomeReplier wires the outbound side: NeedsBinding /
				// AgentOffline / AgentArchived / issue-created translate to a
				// Lark-side reply card. Requires the real APIClient and the
				// binding token service; otherwise it falls back to the noop
				// replier (outcomes logged, not delivered). We only register
				// it on the ResolverSet when it can actually deliver, so a
				// pre-outbound deployment pays no reply-goroutine cost.
				replier := lark.NewLarkOutcomeReplier(lark.OutcomeReplierConfig{
					APIClient:   larkClient,
					BindingSvc:  h.LarkBindingTokens,
					Credentials: installSvc,
					Queries:     queries,
					AppURL:      appURLFromEnv(),
					Logger:      slog.Default(),
				})
				var resolverReplier lark.OutcomeReplier
				if larkClient.IsConfigured() {
					resolverReplier = replier
				}

				// Feishu adapter (MUL-3620): the WSLongConnConnector talks
				// Lark's long-conn protocol over gorilla/websocket and wraps
				// every read with a ctx-cancel watchdog so lease loss /
				// shutdown breaks the blocking ReadMessage in bounded time —
				// the invariant §4.4 leans on. If the endpoint fetcher fails
				// to initialize (bad MULTICA_LARK_CALLBACK_BASE_URL or
				// similar), buildLarkConnector logs and falls back to the
				// NoopConnector so the lease / supervisor lifecycle still runs
				// against real DB rows — inbound messages are silently dropped
				// until the config is fixed, with the boot log labelling the
				// mode "noop".
				//
				// Registering the Factory (connect/send) + ResolverSet
				// (inbound pipeline seams) is all it takes to add the platform
				// to the engine — no engine edit.
				connector, connectorLabel := buildLarkConnector(installSvc, larkClient)
				lark.RegisterFeishu(channelRegistry, lark.FeishuChannelDeps{
					Connector:   connector,
					APIClient:   larkClient,
					Credentials: installSvc,
					Logger:      slog.Default(),
				})
				mediaResolver := lark.NewFeishuMediaResolver(larkClient, installSvc, store, engine.NewDBMediaIntentLedger(queries), slog.Default())
				channelRouter.Register(channel.TypeFeishu, lark.NewFeishuResolverSet(
					cs, feishuSession, auditLogger, resolverReplier, typingIndicator, mediaResolver,
				))
				slog.Info("lark inbound pipeline wired", "connector", connectorLabel)

				// One-shot union_id backfill for installations created
				// before migration 112 added bot_union_id. Runs off the
				// hot startup path so a slow Lark round-trip cannot block
				// HTTP listener boot. New installs already write
				// bot_union_id during the device-flow finalize, so this
				// is bridge code — it will simply find no rows to update
				// on a fresh deployment and exit. MUL-2671.
				go lark.BackfillBotUnionIDs(context.Background(), cs, larkClient, installSvc, slog.Default())

				// Upgrade repair for deployments that ran the whole
				// integration against Lark international via the deployment-
				// wide base-URL override before per-installation region
				// existed: migration 116 backfilled their rows to 'feishu',
				// so relabel them to 'lark' (their true cloud) before the
				// operator clears the override. No-op on mainland / fresh
				// deployments. Off the hot startup path like the union_id
				// backfill. MUL-3083.
				go lark.BackfillRegionFromLegacyOverride(context.Background(), cs,
					strings.TrimSpace(os.Getenv("MULTICA_LARK_HTTP_BASE_URL")),
					strings.TrimSpace(os.Getenv("MULTICA_LARK_CALLBACK_BASE_URL")),
					slog.Default())

				// Device-flow registration service: end-to-end install
				// pipeline that talks to accounts.feishu.cn (RFC 8628)
				// for the QR-scan handshake and then commits the
				// resulting Bot credentials + the installer's
				// lark_user_binding in one DB transaction. The optional
				// MULTICA_LARK_REGISTRATION_DOMAIN / _LARK_DOMAIN env
				// vars override the protocol hosts for staging / dev.
				regCfg := lark.RegistrationConfig{
					Domain:     strings.TrimSpace(os.Getenv("MULTICA_LARK_REGISTRATION_DOMAIN")),
					LarkDomain: strings.TrimSpace(os.Getenv("MULTICA_LARK_REGISTRATION_LARK_DOMAIN")),
				}
				regClient := lark.NewRegistrationClient(regCfg)
				regSvc, rerr := lark.NewRegistrationService(
					lark.RegistrationServiceConfig{Logger: slog.Default()},
					regClient,
					larkClient,
					queries,
					pool,
					installSvc,
					h.LarkBindingTokens,
				)
				if rerr != nil {
					slog.Error("lark: RegistrationService init failed; install disabled", "error", rerr)
				} else {
					// Publish lark_installation:created at row-commit time so the
					// connection badge refreshes on every workspace client, not just
					// the tab that polls the install status to success.
					regSvc.SetEventBus(bus)
					h.LarkRegistration = regSvc
					slog.Info("lark device-flow install enabled")
				}
			}
		}
	} else {
		slog.Info("lark integration disabled (MULTICA_LARK_SECRET_KEY not set)")
	}

	// Slack integration. Multi-tenant B2 model (MUL-3666): Multica hosts ONE
	// Slack app, workspaces self-install via OAuth, and inbound runs on a single
	// deployment-level Socket Mode connection routed by team_id — replacing the
	// stage-3 per-installation connection model (MUL-3516).
	//
	// Two deployment-level env vars gate the two halves:
	//   - MULTICA_SLACK_SECRET_KEY decrypts the per-installation bot token
	//     (xoxb-) stored on the channel_installation row. It gates the inbound
	//     ResolverSet + the outbound reply subscriber, so without it there is no
	//     Slack at all.
	//   - MULTICA_SLACK_APP_TOKEN is the app-level token (xapp-) authorizing the
	//     single Socket Mode connection. It cannot be obtained via OAuth, so it
	//     is a one-time operator config. Without it, inbound is disabled (the
	//     ResolverSet + outbound are still wired so an existing install's replies
	//     keep flowing, but no new events are received).
	//
	// The ResolverSet/Outbound share the same engine.ChatSession, channel_*
	// tables, IssueService and TaskService as Feishu, so /issue, dedup, and
	// run-triggering behave identically. Feishu is untouched. Each Slack
	// installation is a bring-your-own-app (BYO) install carrying its OWN
	// app-level token, so a per-installation Slack Factory is registered and the
	// Supervisor drives one Socket Mode connection per installation (like Feishu).
	if slackKey, err := secretbox.LoadKey("MULTICA_SLACK_SECRET_KEY"); err == nil {
		box, err := secretbox.New(slackKey)
		if err != nil {
			slog.Error("slack: secretbox.New failed; slack integration disabled", "error", err)
		} else {
			// Outbound replier (MUL-3666): delivers NeedsBinding prompt /
			// AgentOffline / AgentArchived / issue-created notices. The binding
			// token service mints the single-use token embedded in the prompt's
			// redeem link; the redeem endpoint (registered below, public) binds
			// the Slack user to their Multica account.
			slackBindingSvc := slack.NewBindingTokenService(queries, pool)
			h.SlackBindingTokens = slackBindingSvc
			slackReplier := slack.NewOutboundReplier(slack.OutboundReplierConfig{
				Binding: slackBindingSvc,
				Decrypt: box.Open,
				// The bind link (/slack/bind) is a web-app page, so it must use the
				// app URL (MULTICA_APP_URL ?? FRONTEND_ORIGIN), NOT MULTICA_PUBLIC_URL
				// (the backend/API URL). Mirrors the Lark replier (appURLFromEnv).
				AppURL:  appURLFromEnv(),
				Queries: queries,
				Logger:  slog.Default(),
			})
			// Typing indicator (MUL-3874): a 👀 reaction on the user's message
			// while the agent works, cleared when the run finishes or fails.
			// Best-effort; failures are logged only. Registered before the
			// outbound reply subscriber so, on EventChatDone, the reaction clears
			// ahead of the reply (bus delivery is synchronous, in subscription
			// order). Subscribing here is also the only path that clears the
			// reaction on a failed run, which the outbound replier does not handle.
			slackTyping := slack.NewTypingIndicatorManager(queries, box.Open, slog.Default())
			slackTyping.Register(bus)
			// Slack attachments require object storage because each chat
			// attachment points to an uploaded object. When storage is disabled,
			// leave the media resolver unset and ingest Slack messages as text.
			var slackMedia engine.MediaResolver
			if store != nil {
				slackMedia = slack.NewMediaResolver(
					box.Open,
					store,
					engine.NewDBMediaIntentLedger(queries),
					slog.Default(),
				)
			}
			channelRouter.Register(slack.TypeSlack, slack.NewSlackResolverSet(queries, pool, slackReplier, slackTyping, slackMedia))
			slack.NewOutbound(queries, box.Open, slog.Default()).Register(bus)

			// On-demand history reader behind the unified `multica chat history`
			// command (MUL-3871): pull the session's Slack conversation when the
			// agent asks, instead of force-assembling it on every inbound.
			h.SlackHistory = slack.NewHistory(queries, box.Open, slog.Default())

			// `/issue`, `/new`, and `/clear` are real Slack slash commands delivered
			// over the same Socket Mode connection. `/issue` enqueues quick-create;
			// the two session controls reuse the shared route/context and task
			// services. Every outcome receives a private ephemeral acknowledgement.
			slackSlash := slack.NewSlashCommandProcessor(slack.SlashCommandConfig{
				Queries: queries,
				Tasks:   h.TaskService,
				Control: slack.NewSlackDMControlStarter(queries, pool, h.TaskService, h),
				Binding: slackBindingSvc,
				AppURL:  appURLFromEnv(),
				Logger:  slog.Default(),
			})

			// Per-installation inbound: the Supervisor builds + supervises one
			// Socket Mode connection per active Slack installation, authenticated
			// with that installation's OWN app-level token (xapp-, pasted at BYO
			// install) — no deployment-level app token, no single connection.
			slack.RegisterSlack(channelRegistry, slack.ChannelDeps{Decrypt: box.Open, Logger: slog.Default(), Slash: slackSlash})

			// BYO self-serve install (paste bot token + app-level token). The
			// InstallService needs only the at-rest encryption key — there is no
			// hosted OAuth client credential.
			installSvc, ierr := slack.NewInstallService(queries, pool, box, slog.Default())
			if ierr != nil {
				slog.Error("slack: InstallService init failed; install disabled", "error", ierr)
			} else {
				h.SlackInstall = installSvc
			}
			slog.Info("slack integration enabled (BYO per-installation socket mode)")
		}
	} else {
		slog.Info("slack integration disabled (MULTICA_SLACK_SECRET_KEY not set)")
	}

	// DingTalk uses one outbound Stream connection per BYO installation. The
	// AppSecret is encrypted at rest and the integration is inert unless its
	// dedicated deployment key is configured.
	if dingtalkKey, err := secretbox.LoadKey("MULTICA_DINGTALK_SECRET_KEY"); err == nil {
		box, err := secretbox.New(dingtalkKey)
		if err != nil {
			slog.Error("dingtalk: secretbox.New failed; integration disabled", "error", err)
		} else {
			dingtalkClient := dingtalk.NewClient(nil, "")
			bindingSvc := dingtalk.NewBindingTokenService(queries, pool)
			h.DingTalkBindingTokens = bindingSvc
			replier := dingtalk.NewOutboundReplier(dingtalk.OutboundReplierConfig{
				Binding: bindingSvc,
				Decrypt: box.Open,
				Client:  dingtalkClient,
				AppURL:  appURLFromEnv(),
				Logger:  slog.Default(),
			})
			ack := dingtalk.NewAckNotifier(dingtalkClient, box.Open, slog.Default())
			var media engine.MediaResolver
			if store != nil {
				media = dingtalk.NewMediaResolver(
					dingtalkClient,
					box.Open,
					store,
					engine.NewDBMediaIntentLedger(queries),
					slog.Default(),
				)
			}
			botNames := dingtalk.NewBotNameResolver(dingtalkClient, box.Open)
			channelRouter.Register(dingtalk.TypeDingTalk, dingtalk.NewDingTalkResolverSet(queries, pool, replier, ack, media, botNames))
			dingtalk.NewOutbound(queries, box.Open, dingtalkClient, slog.Default()).Register(bus)
			dingtalk.RegisterDingTalk(channelRegistry, dingtalk.ChannelDeps{
				Decrypt:  box.Open,
				Client:   dingtalkClient,
				BotNames: botNames,
				Logger:   slog.Default(),
			})
			installSvc, installErr := dingtalk.NewInstallService(queries, pool, box, slog.Default())
			if installErr != nil {
				slog.Error("dingtalk: InstallService init failed; install disabled", "error", installErr)
			} else {
				h.DingTalkInstall = installSvc
			}
			slog.Info("dingtalk integration enabled (BYO per-installation stream mode)")
		}
	} else {
		slog.Info("dingtalk integration disabled (MULTICA_DINGTALK_SECRET_KEY not set)")
	}

	// WeCom smart-bot integration ("智能机器人" / aibot). Per-installation
	// WebSocket long connection to wss://openws.work.weixin.qq.com; the
	// Supervisor drives one connection per active wecom installation, gated
	// by the shared ws_lease_token so multi-replica deployments still hold
	// at most one active socket per bot (WeCom itself only permits one).
	//
	// Gated by MULTICA_WECOM_SECRET_KEY. Without it, the whole block is
	// skipped and the wecom Web-UI endpoints return 503; existing deployments
	// are unaffected. The smart-bot flow does NOT require any public HTTP
	// callback, so nothing else needs to be exposed to the internet.
	if wecomKey, err := secretbox.LoadKey("MULTICA_WECOM_SECRET_KEY"); err == nil {
		box, err := secretbox.New(wecomKey)
		if err != nil {
			slog.Error("wecom: secretbox.New failed; wecom integration disabled", "error", err)
		} else {
			credsResolver, err := wecom.NewSecretboxCredentialsResolver(box)
			if err != nil {
				slog.Error("wecom: credentials resolver init failed; wecom integration disabled", "error", err)
			} else {
				wecomStore := wecom.NewStore(queries)
				h.WecomStore = wecomStore
				h.WecomCredentials = credsResolver

				// Binding tokens back the per-user "link your Multica account"
				// prompt sent to first-time WeCom senders. aibot userids are
				// anonymized T-prefixed ids with no relation to real userids
				// or emails, so an explicit binding table is the only correct
				// answer — see wecom/binding.go for the rationale.
				wecomBinding := wecom.NewBindingTokenService(queries, pool)
				h.WecomBindingTokens = wecomBinding

				// Senders registry: the wecom OutboundReplier is created here
				// at boot, but the live wsSender it needs to push
				// aibot_send_msg only exists inside a running wecomChannel.
				// wecom.NewSendersRegistry mints a shared map; the
				// ChannelDeps write side and the Replier read side both
				// receive it, and each Channel.Connect self-registers on
				// entry and clears on exit.
				wecomSenders := wecom.NewSendersRegistry()

				wecomReplier := wecom.NewOutboundReplier(wecom.OutboundReplierConfig{
					Binding: wecomBinding,
					Senders: wecomSenders,
					AppURL:  appURLFromEnv(),
					Logger:  slog.Default(),
				})

				// Wecom shares the engine.ChatSession (channel_type-keyed) so
				// /issue, dedup, and run-triggering behave identically across
				// platforms. Session titles use the wecom-flavored wording
				// (Chinese product voice — wecom deployments are China-only).
				wecomSession := engine.NewChatSession(queries, pool, wecom.TypeWecom, engine.SessionTitles{
					Group:    "企业微信群聊",
					Direct:   "企业微信单聊",
					Fallback: "企业微信会话",
				})

				wecom.RegisterWecom(channelRegistry, wecom.ChannelDeps{
					Credentials: credsResolver,
					Senders:     wecomSenders,
					Metrics:     wecomMetricsOrNil(opts.WecomMetrics),
					Logger:      slog.Default(),
				})
				// Inbound media: a callback carries a pre-signed COS url and
				// a per-url key, so the resolver needs no WeCom credential —
				// only somewhere durable to put the bytes. Without an object
				// store there is nothing to point an attachment at, so the
				// resolver is left nil and attachments stay as their
				// placeholder text. Same nil-guard as DingTalk above.
				var wecomMedia engine.MediaResolver
				if store != nil {
					wecomMedia = wecom.NewMediaResolver(
						store,
						engine.NewDBMediaIntentLedger(queries),
						wecomSenders,
						slog.Default(),
					)
				}
				channelRouter.Register(wecom.TypeWecom, wecom.NewResolverSet(
					wecomStore, wecomSession, wecomReplier, wecomMedia,
				))

				// EventChatDone subscriber: pushes the agent's chat reply
				// back over the same aibot WebSocket the inbound loop owns.
				// Mirrors slack.NewOutbound(...).Register(bus). Without it
				// the agent's reply lands only in Multica's web UI — the
				// user in WeCom sees no response.
				//
				// WithAttachments adds the second hop: the files the agent
				// bound to that reply are read back out of object storage and
				// sent into the chat behind it. Passed only when this
				// deployment configured storage — with none there is nothing
				// to read an attachment out of, and the option is what the
				// delivery path checks for.
				//
				// DeclareChannelFileDelivery is the same condition said to the
				// agent: a run only gets told it can send a file where this
				// branch actually built the hop that sends it. The two lines
				// sit together on purpose — a deployment that has the storage
				// and a deployment whose agents are promised delivery must be
				// the same deployment, and the only way to keep that true is
				// for one `if` to decide both.
				wecomOutboundOpts := []wecom.OutboundOption{}
				if store != nil {
					wecomOutboundOpts = append(wecomOutboundOpts, wecom.WithAttachments(store))
					h.DeclareChannelFileDelivery(string(wecom.TypeWecom))
				}
				wecom.NewOutbound(queries, wecomSenders, slog.Default(), wecomOutboundOpts...).Register(bus)

				// Ranges the media fetcher may dial despite looking reserved.
				// Empty by default, which leaves the SSRF guard exactly as
				// strict as it ships. A deployment behind a fake-IP proxy
				// needs it: there, every public hostname resolves into the
				// proxy's pool (198.18.0.0/15 is the common one), so WeCom's
				// own COS host is indistinguishable from a metadata endpoint
				// by address alone and every attachment is refused.
				if raw := strings.TrimSpace(os.Getenv("MULTICA_WECOM_MEDIA_ALLOW_CIDRS")); raw != "" {
					for _, err := range wecom.SetMediaAllowedPrefixes(strings.Split(raw, ",")) {
						slog.Error("wecom: ignoring malformed media allow cidr", "error", err)
					}
					slog.Warn("wecom: media guard has an operator allow-list; those ranges are reachable by a URL WeCom supplies",
						"cidrs", raw)
				}

				// Frame tracing: off unless an operator asks for it. It
				// records a bounded prefix of message text, so the fact that
				// it is on has to be visible in the log it is writing into —
				// otherwise a session gets left switched on and nobody
				// notices message content accumulating.
				if wecom.SetTrace(os.Getenv("MULTICA_WECOM_TRACE") == "1") {
					slog.Warn("wecom: frame tracing ON — records message text; unset MULTICA_WECOM_TRACE when done")
				}

				slog.Info("wecom integration enabled (smart bot, long connection)")
				// SINGLE-REPLICA CONSTRAINT: WeCom outbound (agent replies +
				// inbox pushes) is delivered only by the replica holding each
				// bot's in-process WebSocket lease. On a multi-replica
				// deployment, an EventChatDone/EventInboxNew published on another
				// replica cannot reach the lease holder, so those replies are
				// dropped. This is stated conditionally rather than gated on a
				// replica-count signal: the server has no reliable count here,
				// and REDIS_URL means "Redis configured" (it also gates rate
				// limiting), not "more than one replica". See wecom/outbound.go
				// and SELF_HOSTING.md. Remove once outbound routes to the lease
				// holder.
				slog.Warn("wecom integration: WeCom agent replies and inbox pushes are delivered only by the replica holding each bot's WebSocket lease. If you run more than one backend replica, responses produced on a replica that does not hold the lease will be dropped — run the WeCom-enabled backend as a single replica until cross-replica outbound routing is implemented.")
			}
		}
	} else {
		slog.Info("wecom integration disabled (MULTICA_WECOM_SECRET_KEY not set)")
	}

	// Telegram integration. Same shape as Slack: BYO bot token pasted at
	// install, one getUpdates long-polling loop per active installation
	// supervised by the shared engine.Supervisor, resolvers on the generic
	// channel_* tables, outbound streaming via throttled editMessageText on
	// the event bus. Gated by MULTICA_TELEGRAM_SECRET_KEY (the at-rest token
	// encryption key); when unset the handlers return 503 and no Factory is
	// registered.
	if telegramKey, err := secretbox.LoadKey("MULTICA_TELEGRAM_SECRET_KEY"); err == nil {
		box, err := secretbox.New(telegramKey)
		if err != nil {
			slog.Error("telegram: secretbox.New failed; telegram integration disabled", "error", err)
		} else {
			telegramBindingSvc := telegram.NewBindingTokenService(queries, pool)
			h.TelegramBindingTokens = telegramBindingSvc
			telegramReplier := telegram.NewOutboundReplier(telegram.OutboundReplierConfig{
				Binding: telegramBindingSvc,
				Decrypt: box.Open,
				// The bind link (/telegram/bind) is a web-app page: app URL, not
				// the API URL. Mirrors the Slack replier.
				AppURL: appURLFromEnv(),
				Logger: slog.Default(),
			})
			telegramTyping := telegram.NewTypingNotifier(box.Open, "", nil, slog.Default())
			channelRouter.Register(telegram.TypeTelegram, telegram.NewTelegramResolverSet(queries, pool, telegramReplier, telegramTyping))
			telegramOutbound := telegram.NewOutbound(queries, box.Open, "", nil, slog.Default())
			telegramOutbound.Register(bus)
			h.TelegramOutbound = telegramOutbound

			// Per-installation inbound: the Supervisor builds + supervises one
			// long-polling loop per active Telegram installation.
			telegram.RegisterTelegram(channelRegistry, telegram.ChannelDeps{Decrypt: box.Open, Logger: slog.Default()})

			installSvc, ierr := telegram.NewInstallService(queries, pool, box, slog.Default())
			if ierr != nil {
				slog.Error("telegram: InstallService init failed; install disabled", "error", ierr)
			} else {
				h.TelegramInstall = installSvc
			}
			slog.Info("telegram integration enabled (per-installation long polling)")
		}
	} else {
		slog.Info("telegram integration disabled (MULTICA_TELEGRAM_SECRET_KEY not set)")
	}

	// Composio integration (MUL-3720). Gated by COMPOSIO_API_KEY plus the
	// composio_mcp_apps feature flag. The env var is the project-scoped key the
	// standalone SDK authenticates Composio with (sent as x-api-key; the project
	// is resolved from the key, so NO project id is configured). When unset or
	// flag-disabled the whole block is skipped and the composio HTTP handlers
	// return 503; existing deployments are unaffected. An operator opts in by
	// setting COMPOSIO_API_KEY plus a callback base
	// (COMPOSIO_CALLBACK_BASE_URL, falling back to MULTICA_PUBLIC_URL). The
	// toolkit→auth-config mapping is NOT configured here — it is resolved
	// dynamically from the project's /auth_configs at request time, so enabling
	// a toolkit is a dashboard action, not a redeploy. State signing uses
	// COMPOSIO_STATE_SECRET, or a key derived from JWT_SECRET when that is unset.
	if composioAPIKey := strings.TrimSpace(os.Getenv("COMPOSIO_API_KEY")); composioAPIKey != "" {
		if !featureflags.ComposioMCPAppsEnabled(context.Background(), opts.FeatureFlags) {
			slog.Info("composio integration disabled (feature flag off)")
		} else {
			sdkClient, err := composiosdk.NewClient(composiosdk.Options{APIKey: composioAPIKey})
			if err != nil {
				slog.Error("composio: SDK client init failed; composio integration disabled", "error", err)
			} else {
				stateSecret := composioStateSecret()
				callbackBase := composioCallbackBaseURL(signupConfig.PublicURL)
				switch {
				case len(stateSecret) == 0:
					slog.Error("composio: no state secret (set COMPOSIO_STATE_SECRET or JWT_SECRET); composio integration disabled")
				case callbackBase == "":
					slog.Error("composio: no callback base url (set COMPOSIO_CALLBACK_BASE_URL or MULTICA_PUBLIC_URL); composio integration disabled")
				default:
					svc, serr := composiointeg.NewService(sdkClient, queries, composiointeg.Config{
						StateSecret:     stateSecret,
						CallbackBaseURL: callbackBase,
						FrontendBaseURL: appURLFromEnv(),
					})
					if serr != nil {
						slog.Error("composio: service init failed; composio integration disabled", "error", serr)
					} else {
						h.Composio = svc
						// Stage 3 (MUL-3721) hook: feed the per-task MCP
						// overlay builder into TaskService so every Enqueue*
						// path attaches the initiator user's Composio session
						// URL to the task row before the daemon claims it.
						// taskSvc already exists by this point — it was
						// constructed inside NewHandler — and exposes its
						// Composio field for exactly this kind of late wiring,
						// so no Handler-level mutation is needed.
						if h.TaskService != nil {
							h.TaskService.Composio = svc
						}
						slog.Info("composio integration enabled")
					}
				}
			}
		}
	} else {
		slog.Info("composio integration disabled (COMPOSIO_API_KEY not set)")
	}

	// VCS at-rest encryption: the box encrypts per-workspace access tokens and
	// webhook secrets for token-based providers (Forgejo / Gitea / GitLab).
	// Without it, connect/webhook handlers return 503 (so a misconfigured
	// self-host never stores plaintext secrets).
	if vcsKey, err := secretbox.LoadKey("MULTICA_VCS_SECRET_KEY"); err == nil {
		box, err := secretbox.New(vcsKey)
		if err != nil {
			slog.Error("vcs: secretbox.New failed; vcs integration disabled", "error", err)
		} else {
			h.VCSSecretBox = box
			slog.Info("vcs integration enabled")
		}
	} else {
		slog.Info("vcs integration disabled (MULTICA_VCS_SECRET_KEY not set)")
	}

	// Plugin secrets use a dedicated deployment key. Keeping this separate from
	// VCS and channel secrets gives operators an isolated rotation and blast
	// radius; without it, saving a `secret` config field fails closed rather
	// than storing plaintext.
	if pluginKey, err := secretbox.LoadKey("MULTICA_PLUGIN_SECRET_KEY"); err == nil {
		box, err := secretbox.New(pluginKey)
		if err != nil {
			slog.Error("plugins: secretbox.New failed; Plugin secrets disabled", "error", err)
		} else if h.PluginService != nil {
			h.PluginService.Secrets = box
			// The same deployment key, kept raw as well. Sealing and signing
			// need different things from it: a secret config value is sealed
			// and later opened, while a hook signature must be REPRODUCED on
			// demand, which a box cannot do. Each installation's signing secret
			// is derived from this rather than stored, so no row holds a usable
			// one.
			h.PluginService.DeploymentKey = pluginKey
			h.PluginSurfaceTokens, err = handler.NewPluginSurfaceTokenBox(pluginKey)
			if err != nil {
				slog.Error("plugins: surface token key derivation failed; surfaces disabled", "error", err)
			}
			slog.Info("Plugin secret encryption enabled")
		}
	} else {
		slog.Info("Plugin secrets disabled (MULTICA_PLUGIN_SECRET_KEY not set)")
	}

	// Hook engine. Event-triggered hooks are dispatched off the bus onto a
	// worker pool: Bus.Publish runs listeners inline on the publishing request's
	// goroutine, so anything that dials a third-party endpoint from there would
	// put an outside server on the critical path of creating an issue.
	if h.PluginService != nil {
		h.PluginService.Callbacks = service.NewCallbackTokens()
		// Omitted rather than sent relative: a third-party hook server cannot
		// call a path-only URL, and a broken absolute URL is harder to diagnose
		// than an absent one.
		if baseURL := pluginActionBaseURL(signupConfig.PublicURL); baseURL != "" {
			h.PluginService.CallbackBaseURL = baseURL
		} else {
			slog.Warn("plugins: MULTICA_PLUGIN_API_URL and MULTICA_PUBLIC_URL are not set; hook callbacks will carry no callback_url")
		}
		// The flag reaches the event path only through the service: a worker has
		// no request to read it from.
		h.PluginService.FeatureFlags = h.FeatureFlags
		pluginEvents := service.NewPluginEventDispatcher(h.PluginService)
		service.SubscribePluginEvents(bus, pluginEvents)
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
	// Fleet. Returns nil when no Cloud URL is configured — the Auth /
	// DaemonAuth middlewares treat nil as "mcn_ not supported" and
	// reject with 401, instead of falling through to mul_/JWT paths.
	// Reuses MULTICA_CLOUD_URL (the same URL the cloud-runtime proxy uses) so a
	// deployment has one authoritative multica-cloud connection.
	cloudPATVerifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{
		FleetBaseURL: signupConfig.CloudURL,
		Redis:        rdb,
	})

	// Empty-claim cache: lets the daemon poll path skip a Postgres
	// scan when a recent check confirmed the runtime had no queued
	// task. Returns nil when rdb is nil — TaskService treats that
	// as "no cache, always hit DB" (existing behavior).
	h.TaskService.EmptyClaim = service.NewEmptyClaimCache(rdb)
	// Stale-dispatch reclaim has a separate schedule because an empty queued
	// verdict cannot represent a claim response lost after commit. Missing or
	// failed Redis state keeps the historical PostgreSQL fallback.
	h.TaskService.ReclaimCheck = service.NewReclaimCheckCache(rdb)

	// Wire WS heartbeat after stores are finalized so the WS path uses the
	// same (possibly Redis-backed) stores as the HTTP path.
	daemonHub.SetHeartbeatHandler(h.HandleDaemonWSHeartbeat)
	// WS-first claim (MUL-4257): route daemon:rpc_request frames (e.g.
	// tasks.claim) through the same handlers as the HTTP endpoints.
	daemonHub.SetRPCHandler(h.DaemonRPCHandler)
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
	r.Use(h.PluginSurfaceHostBoundary)
	r.Use(middleware.ContentSecurityPolicy)

	// Share allowed origins with WebSocket origin checker.
	realtime.SetAllowedOrigins(origins)

	// Share the same trusted-proxy CIDRs (MULTICA_TRUSTED_PROXIES) so the
	// WebSocket origin check honors X-Forwarded-Host only from trusted proxies,
	// using one config source instead of a parallel one.
	realtime.SetTrustedProxies(signupConfig.TrustedProxies)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   corsAllowedHeaders,
		ExposedHeaders:   corsExposedHeaders,
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

	// Local file serving (when using local storage). Served through the
	// handler so /uploads/* carries the same preview security headers as the
	// /api/attachments download endpoint; self-hosted split-origin/same-origin
	// clients can then iframe-preview PDFs/HTML fetched straight from the
	// static route instead of hitting the global frame-ancestors 'none' CSP.
	// See MUL-3821 / #4477.
	if _, ok := store.(*storage.LocalStorage); ok {
		r.Get("/uploads/*", h.ServeLocalUpload)
	}

	// Capability-authenticated attachment download (MUL-5292). Public by
	// necessity: a native download (Electron's webContents.downloadURL, a
	// cross-site webview <img>) carries neither Authorization nor a session
	// cookie, so there is nothing here for middleware.Auth to read. The
	// short-lived, single-attachment signature in the query is the credential,
	// and it is only ever minted by the AUTHENTICATED GET
	// /api/attachments/{id} after that request's membership check passed.
	// The authenticated /api/attachments/{id}/download route below is
	// unchanged — this one is purely additive.
	r.Get("/api/attachments/{id}/signed-download", h.DownloadAttachmentWithCapability)

	// Avatar serving. Public for the same reason as the capability download
	// above: the auth cookie is SameSite=Strict, so an auth-gated URL cannot
	// be a native <img src> from Desktop / mobile webview or a split-origin
	// self-hosted web app. The HMAC signature in the path is the credential.
	// It covers the storage key, only image keys resolve, and the object must
	// be avatar-class — see server/internal/handler/avatar.go (MUL-5393 /
	// #6024).
	r.Get("/api/avatars/{sig}/*", h.ServeAvatar)

	// Hosted plugin documents are capability-authenticated and intentionally
	// outside the session middleware: the configured content origin must stay
	// cookie-free. The encrypted path token binds the installation, immutable
	// version, surface and one bridge challenge.
	r.Get("/plugin-surfaces/{token}", h.ServePluginSurface)

	// Auth (public) — per-IP rate limiting.
	if rdb == nil {
		slog.Warn("auth rate limiting disabled: REDIS_URL not configured")
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
	r.With(contactSalesRL).Post("/api/contact-sales", h.CreateContactSales)
	// Public share-link preview — no auth: shows the workspace name/slug and
	// inviter so a not-yet-logged-in visitor can see what they're joining.
	r.Get("/api/share-links/{code}", h.GetShareLinkInfo)

	// Webhook ingress for autopilots. Outside the authenticated group on
	// purpose: the bearer token in the URL path IS the credential. Workspace
	// context is derived from the trigger row, never from request headers.
	r.Post("/api/webhooks/autopilots/{token}", h.HandleAutopilotWebhook)
	// GitHub App webhook (no Multica auth — requests are authenticated via
	// HMAC-SHA256 signature in the handler) and post-install setup callback.
	r.Post("/api/webhooks/github", h.HandleGitHubWebhook)
	r.Get("/api/github/setup", h.GitHubSetupCallback)
	// Slack OAuth callback (no Multica auth in the path — it is hit by Slack's
	// browser redirect; the workspace/agent/initiator are recovered from the
	// sealed state). It exchanges the code, upserts the install, then bounces
	// the browser back to Settings → Integrations.
	// VCS webhook for token-based providers (Forgejo / Gitea / GitLab). No Multica
	// auth — authenticated per-connection by the provider's signature scheme;
	// the connection id in the path selects the workspace, provider, and
	// decryption secret.
	r.Post("/api/webhooks/vcs/{connectionId}", h.HandleVCSWebhook)
	// Stripe webhook (no Multica auth — Stripe signs the raw body
	// with a shared secret, the multica-cloud upstream verifies. We
	// only forward the bytes + the Stripe-Signature header; see
	// HandleCloudBillingStripeWebhook for the rationale).
	r.Post("/api/webhooks/stripe", h.HandleCloudBillingStripeWebhook)

	// Composio OAuth callback (MUL-3843). NOT under the Auth group on purpose:
	// Composio 302-redirects the user's browser here at the end of the OAuth
	// flow, and the cookie session is frequently absent (expired session,
	// SameSite=Strict / Safari ITP stripping cross-site cookies, private
	// windows, self-hosted callbacks on a different subdomain). Identity is NOT
	// taken from the session — it comes from the HMAC-signed `state` query
	// param, which CompleteCallback verifies (signature, expiry, replay) before
	// doing anything. h.Composio == nil still returns 503. Keeping it inside the
	// Auth group made a missing cookie a hard 401, breaking the flow for exactly
	// the browsers above; the other four composio endpoints stay session-gated.
	r.Get("/api/integrations/composio/callback", h.ComposioCallback)

	// Daemon API routes (require daemon token or valid user token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries, patCache, daemonTokenCache, cloudPATVerifier))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/ws", h.DaemonWebSocket)
		r.Get("/workspaces", h.ListDaemonWorkspaces)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)
		r.Get("/workspaces/{workspaceId}/runtime-profiles", h.DaemonListRuntimeProfiles)

		// Agent-triggered plugin hooks. The daemon's local MCP server calls
		// this when an agent picks one of its tools; the server makes the
		// signed request so the daemon never holds the signing secret.
		r.Post("/tasks/{id}/plugin-hooks", h.InvokeAgentPluginHook)
		// The broker asks for an mcp hook's credential at connection time, so
		// a secret never sits in a task record.
		r.Get("/tasks/{id}/plugin-mcp/{contributionId}/credential", h.ResolvePluginMCPCredential)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		// Canonical machine-level batch claim (MUL-4257). `/claim` is a
		// transitional alias; the daemon coordinator targets the canonical
		// path.
		r.Post("/tasks/claim", h.ClaimTasksByRuntime)
		r.Post("/claim", h.ClaimTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/tasks/{taskId}/prepare-lease", h.ExtendTaskPrepareLease)
		r.Post("/runtimes/{runtimeId}/tasks/{taskId}/skill-bundles/resolve", h.ResolveTaskSkillBundles)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)
		r.Post("/runtimes/{runtimeId}/models/{requestId}/result", h.ReportModelListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/{requestId}/result", h.ReportLocalSkillListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/import/{requestId}/result", h.ReportLocalSkillImportResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/wait-local-directory", h.MarkTaskWaitingLocalDirectory)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/usage", h.ReportTaskUsage)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)
		r.Post("/tasks/{taskId}/cancel-ack", h.AckTaskCancelled)

		r.Post("/workspaces/{workspaceId}/issues/gc-check", h.BatchIssueGCCheck)
		r.Get("/issues/{issueId}/gc-check", h.GetIssueGCCheck)
		r.Get("/chat-sessions/{sessionId}/gc-check", h.GetChatSessionGCCheck)
		r.Get("/autopilot-runs/{runId}/gc-check", h.GetAutopilotRunGCCheck)
		r.Get("/tasks/{taskId}/gc-check", h.GetTaskGCCheck)

		r.Post("/runtimes/{runtimeId}/recover-orphans", h.RecoverOrphanedTasks)
		r.Post("/tasks/{taskId}/session", h.PinTaskSession)
	})

	// Public Plugin Action API. This is the stable, globally versioned contract
	// exposed on the Plugin API origin. It accepts only mpi_/mpc_ bearer tokens;
	// browser sessions use the bridge group below and cannot cross this trust
	// boundary even when both hostnames route to the same Go service.
	r.Group(func(r chi.Router) {
		r.Use(middleware.PluginBearerOnly)
		r.Use(middleware.PluginRateLimit(rdb, envPositiveInt("RATE_LIMIT_PLUGIN_API", 120), time.Minute))
		r.Route(publicapiv1.BasePath, func(r chi.Router) {
			registerPluginActionRoutes(r, h)
			r.NotFound(publicapiv1.NotFound)
			r.MethodNotAllowed(publicapiv1.MethodNotAllowed)
		})
	})

	// Browser-session relay for sandboxed plugin surfaces. The iframe talks to
	// the host over MessagePort; the host calls this internal route with the
	// signed-in user's session and the installation header. Keeping it outside
	// /v1 prevents the Public API from accepting session cookies.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache, cloudPATVerifier))
		r.Route(pluginBridgePrefix, func(r chi.Router) {
			registerPluginActionRoutes(r, h)
			// ui / manual only. `event` is dispatched by the host off the event
			// bus; `agent` arrives over MCP rather than this HTTP endpoint.
			r.Post("/hooks/{key}", h.InvokePluginHook)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache, cloudPATVerifier))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// Plugin Action API. Called by the HOST PAGE on the signed-in user's
		// session after a surface asks for something over the postMessage
		// bridge — the iframe holds no credential and never reaches these
		// directly. Which installation is speaking arrives in a header the
		// host sets; the workspace is derived from that installation rather
		// than trusted from the client, and membership is then checked
		// against it. Sits in the user-scoped group for that reason: there is
		// no workspace in the path to gate on.
		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
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
		r.Post("/api/upload-file", h.UploadFile)
		r.Post("/api/feedback", h.CreateFeedback)
		r.With(handler.RequireHumanActor).Post("/api/client-usage", h.UpsertClientUsage)

		// Note (MUL-4309): the generic OpenAI-compatible passthrough endpoints
		// (POST /api/llm/v1/chat/completions[/stream]) were intentionally
		// removed. Exposing a general LLM proxy backed by the deployment's own
		// key let any logged-in user run arbitrary completions on our dime.
		// LLM access is now server-internal only (see pkg/llm); anything the
		// web/client needs must go through a purpose-built business endpoint
		// that fixes the prompt/model server-side (e.g. chat title generation).

		// Attachment download — user-scoped (auth-only), NOT
		// workspace-scoped. The handler self-resolves the workspace
		// from the attachment row and enforces membership inside, so
		// this route is callable as a native browser <img>/<video>
		// src that cannot attach X-Workspace-Slug / X-Workspace-ID
		// headers. Persisting `/api/attachments/<id>/download` into
		// comment markdown depends on this — see MUL-3130. The
		// metadata / delete endpoints below stay workspace-scoped
		// because they are JSON-API consumers that always have
		// workspace context.
		r.Get("/api/attachments/{id}/download", h.DownloadAttachment)

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
					// Listing GitHub installations is member-visible so the
					// integrations tab no longer renders blank for non-admins;
					// the handler strips the management handle and adds a
					// can_manage hint so the UI can gate connect/disconnect.
					r.Get("/github/installations", h.ListGitHubInstallations)
					// VCS connections (Forgejo / Gitea / GitLab) — member-visible
					// for the same reason as GitHub installations; connect /
					// disconnect are admin-gated in the group below.
					r.Get("/vcs/connections", h.ListVCSConnections)
					// Custom runtime profiles — listing/reading is member-visible
					// (the Runtime page renders for everyone; create/edit/delete
					// are admin-gated below).
					r.Get("/runtime-profiles", h.ListRuntimeProfiles)
					r.Get("/runtime-profiles/{profileId}", h.GetRuntimeProfile)
					// The workspace MCP library — member-visible so an agent
					// owner can see what is available to add to their agent.
					// The payload is names and transports only; the stored
					// entries are write-only.
					r.Get("/mcp-servers", h.ListWorkspaceMcpServers)
					// Installed Plugins are member-visible so a member can
					// see what is mounted in their workspace and which scopes
					// it holds; install / configure / remove stay admin-only.
					r.Get("/plugins", h.ListPlugins)
					// One short-lived hosted surface launch. Member-visible
					// because opening an issue is what asks for it; executable
					// bytes stay off the authenticated app/API origin.
					r.Get("/plugins/{installationId}/surfaces/{surfaceKey}/launch", h.GetPluginSurfaceLaunch)
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
					// Curating the shared MCP library is an admin action.
					// Creating an entry binds it to no agent; an agent owner
					// adds it to their own agent through the agent routes.
					r.Post("/mcp-servers", h.CreateWorkspaceMcpServer)
					r.Put("/mcp-servers/{serverId}", h.UpdateWorkspaceMcpServer)
					r.Delete("/mcp-servers/{serverId}", h.DeleteWorkspaceMcpServer)
					r.Post("/share-links", h.CreateShareLink)
					r.Delete("/share-links/{linkId}", h.RevokeShareLink)
					r.Get("/share-links", h.ListShareLinks)
					// Custom runtime profile mutations (admin-only).
					r.Post("/runtime-profiles", h.CreateRuntimeProfile)
					r.Patch("/runtime-profiles/{profileId}", h.UpdateRuntimeProfile)
					r.Put("/runtime-profiles/{profileId}", h.UpdateRuntimeProfile)
					r.Delete("/runtime-profiles/{profileId}", h.DeleteRuntimeProfile)
					// Publishing. The author uploads an artifact bundle and we
					// store it; a version is immutable once published, so
					// there is no update route here by design.
					r.Get("/plugins/packages", h.ListPluginPackages)
					r.Post("/plugins/packages", h.PublishPluginPackage)
					r.Post("/plugins/packages/local", h.PublishLocalPluginPackage)
					r.Delete("/plugins/packages/{packageId}", h.DeletePluginPackage)
					// Installing a Plugin is two steps on purpose: preview
					// reads the published version's manifest and returns the
					// scope list without writing anything, so the consent
					// screen has something to show before an installation
					// exists. Both steps name the same version id, which is
					// what makes the approved manifest and the running code
					// the same artifact.
					r.Post("/plugins/preview", h.PreviewPlugin)
					r.Post("/plugins", h.InstallPlugin)
					r.Get("/plugins/{installationId}/invocations", h.ListPluginInvocations)
					r.Post("/plugins/{installationId}/token", h.RotatePluginToken)
					r.Delete("/plugins/{installationId}/token", h.RevokePluginToken)
					// mcp-transport approval. Discovery adopts nothing; the PUT
					// is the grant, and it pins the tools by schema digest.
					r.Get("/plugins/{installationId}/mcp/{hookKey}/tools", h.ListPluginMCPTools)
					r.Put("/plugins/{installationId}/mcp/{hookKey}/tools", h.ApprovePluginMCPTools)
					r.Put("/plugins/{installationId}/config", h.ConfigurePlugin)
					r.Post("/plugins/{installationId}/enable", h.EnablePlugin)
					r.Post("/plugins/{installationId}/disable", h.DisablePlugin)
					r.Delete("/plugins/{installationId}", h.UninstallPlugin)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)

				// GitHub integration — connect / disconnect remain admin-only;
				// the read-only list endpoint lives in the member-level group
				// above so non-admins can see the workspace's connection state.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Get("/github/connect", h.GitHubConnect)
					r.Get("/github/installations/{installationId}/repositories", h.ListGitHubInstallationRepositories)
					r.Delete("/github/installations/{installationId}", h.DeleteGitHubInstallation)
					// VCS connect / disconnect / webhook regeneration (admin-only).
					r.Post("/vcs/connections", h.ConnectVCS)
					r.Post("/vcs/connections/{connectionId}/rotate-webhook", h.RotateVCSConnectionWebhook)
					r.Delete("/vcs/connections/{connectionId}", h.DeleteVCSConnection)
				})

				// Lark integration. Every endpoint here only requires
				// workspace membership at the router; the real authorization
				// is per-agent and enforced inside each handler via
				// canManageAgent (agent owner OR workspace owner/admin), so an
				// agent's owner can bind/manage their own agent's Bot without
				// being a workspace admin (MUL-4213). The router can't make
				// that call itself: begin identifies the agent by an
				// `agent_id` query param and revoke by an installation id,
				// neither of which is a URL param the role middleware sees.
				//   - Listing stays member-visible (same rationale as GitHub:
				//     the Integrations tab must render for non-admins so they
				//     see "wired up by whom").
				//   - Begin / status / revoke each load the target agent and
				//     run canManageAgent (status gates on the session
				//     initiator or an admin) before doing anything.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/lark/installations", h.ListLarkInstallations)
					r.Delete("/lark/installations/{installationId}", h.RevokeLarkInstallation)
					// Device-flow scan-to-install. Begin opens a new
					// registration session against Lark and returns
					// the QR-code URL; the frontend dialog then polls
					// /install/{sessionId}/status until success or
					// terminal failure.
					r.Post("/lark/install/begin", h.BeginLarkInstall)
					r.Get("/lark/install/{sessionId}/status", h.GetLarkInstallStatus)
				})

				// Slack integration (MUL-3666). Same admin/member split as
				// Lark: listing is member-visible; OAuth begin + revoke are
				// admin-only. The OAuth callback itself is a public route (it is
				// hit by Slack's browser redirect with no workspace in the path)
				// and is registered outside this workspace group.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/slack/installations", h.ListSlackInstallations)
					r.Get("/wecom/installations", h.ListWecomInstallations)
				})
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Delete("/slack/installations/{installationId}", h.RevokeSlackInstallation)
					r.Post("/slack/install/byo", h.RegisterSlackBYO)
					r.Delete("/wecom/installations/{installationId}", h.RevokeWecomInstallation)
					r.Post("/wecom/install/byo", h.RegisterWecomBYO)
				})

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/dingtalk/installations", h.ListDingTalkInstallations)
					r.Get("/dingtalk/groups", h.ListDingTalkGroups)
					r.Delete("/dingtalk/installations/{installationId}/groups/{conversationId}", h.ForgetDingTalkGroup)
					r.Delete("/dingtalk/installations/{installationId}", h.RevokeDingTalkInstallation)
					r.Post("/dingtalk/install/byo", h.RegisterDingTalkBYO)
				})

				// Telegram integration. Same admin/member split as Slack:
				// listing is member-visible; install + revoke are admin-only.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/telegram/installations", h.ListTelegramInstallations)
				})
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Delete("/telegram/installations/{installationId}", h.RevokeTelegramInstallation)
					r.Post("/telegram/install", h.RegisterTelegramBot)
				})
			})
		})

		// Lark binding-token redemption. NOT workspace-scoped because
		// the redeemer hits this BEFORE they have any workspace
		// context — the redemption itself is what mints their
		// lark_user_binding row. Identity comes from the session;
		// the token only proves "this open_id requested binding," and
		// is combined with the logged-in user to create the mapping.
		r.Post("/api/lark/binding/redeem", h.RedeemLarkBindingToken)
		// Slack binding-token redemption. Same rationale as Lark: NOT
		// workspace-scoped because the redeemer hits this before they have any
		// workspace context — the redemption itself mints their binding row. The
		// logged-in user (from the session) is bound to the Slack id the token
		// carries.
		r.Post("/api/slack/binding/redeem", h.RedeemSlackBindingToken)
		// DingTalk binding redemption is user-scoped for the same reason as
		// Slack: the token is redeemed before workspace context is selected.
		r.Post("/api/dingtalk/binding/redeem", h.RedeemDingTalkBindingToken)
		// WeCom smart-bot binding-token redemption. Same rationale as
		// Lark/Slack: the session is the source of truth for the redeemer's
		// Multica identity; the token only carries the WeCom userid to bind.
		r.Post("/api/wecom/binding/redeem", h.RedeemWecomBindingToken)
		// Telegram binding-token redemption. Same rationale: not
		// workspace-scoped, identity from the session, token proves only
		// "this Telegram user id requested binding".
		r.Post("/api/telegram/binding/redeem", h.RedeemTelegramBindingToken)

		// Composio integration (MUL-3720). User-scoped (no workspace context):
		// a connection belongs to a user. These four require a logged-in
		// session; the OAuth callback is the outlier and lives outside the Auth
		// group (registered above with the other public OAuth/webhook routes —
		// see MUL-3843). All return 503 when COMPOSIO_API_KEY is unset.
		r.Route("/api/integrations/composio", func(r chi.Router) {
			r.Post("/connect/init", h.ComposioConnectInit)
			r.Get("/toolkits", h.ListComposioToolkits)
			r.Get("/connections", h.ListComposioConnections)
			r.Delete("/connections/{id}", h.DeleteComposioConnection)
		})

		// User-scoped invitation routes (no workspace context required)
		r.Get("/api/invitations", h.ListMyInvitations)
		r.Get("/api/invitations/{id}", h.GetMyInvitation)
		r.Post("/api/invitations/{id}/accept", h.AcceptInvitation)
		r.Post("/api/invitations/{id}/decline", h.DeclineInvitation)
		r.Post("/api/share-links/join", h.JoinByShareLink)

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

		// Workspace subscriptions use the same cloud transport and Stripe
		// webhook as the existing owner-credit billing surface, but every request
		// is workspace-scoped. Entitlements, summary and prices are
		// member-readable; Checkout, seat reconcile, and Portal mutations require
		// owner/admin. The handlers also enforce
		// billing_workspace_subscriptions so a route refactor cannot
		// accidentally bypass the rollout flag.
		r.Route("/api/cloud-subscriptions", func(r chi.Router) {
			r.Use(handler.RequireHumanActor)

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireWorkspaceMember(queries))
				r.Get("/entitlements", h.GetCloudWorkspaceEntitlements)
				r.Get("/summary", h.GetCloudWorkspaceSubscriptionSummary)
				r.Get("/prices", h.GetCloudWorkspaceSubscriptionPrices)
			})
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
				r.Post("/checkout-sessions", h.CreateCloudWorkspaceSubscriptionCheckout)
				r.Post("/seats/purchase-preview", h.PreviewCloudWorkspaceSubscriptionSeatPurchase)
				r.Post("/seats/purchases", h.PurchaseCloudWorkspaceSubscriptionSeats)
				r.Post("/seats/reconcile", h.ReconcileCloudWorkspaceSubscriptionSeats)
				r.Post("/portal-sessions", h.CreateCloudWorkspaceSubscriptionPortal)
			})
		})

		// --- Workspace-scoped routes (all require workspace membership) ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			// Assignee frequency
			r.Get("/api/assignee-frequency", h.GetAssigneeFrequency)

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/window-usage", h.GetIssueWindowUsage)
				r.Post("/table/groups", h.ListIssueTableGroups)
				r.Post("/table/rows", h.ListIssueTableRows)
				r.Post("/table/facets", h.ListIssueTableFacets)
				r.Get("/search", h.SearchIssues)
				r.Get("/child-progress", h.ChildIssueProgress)
				r.Get("/children", h.ListChildrenByParents)
				r.Get("/grouped", h.ListGroupedIssues)
				r.Get("/", h.ListIssues)
				// POST twin of GET /api/issues for oversized filter sets
				// (agents-working ids facet) — see QueryIssues.
				r.Post("/query", h.QueryIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/quick-create", h.QuickCreateIssue)
				r.Post("/preview-trigger", h.PreviewIssueTrigger)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssue)
					r.Put("/", h.UpdateIssue)
					r.Post("/move", h.MoveIssue)
					r.Delete("/", h.DeleteIssue)
					r.Post("/comments/trigger-preview", h.PreviewCommentTriggers)
					r.Post("/comments", h.CreateComment)
					r.Get("/comments", h.ListComments)
					r.Get("/timeline", h.ListTimeline)
					r.Get("/subscribers", h.ListIssueSubscribers)
					r.Post("/subscribe", h.SubscribeToIssue)
					r.Post("/unsubscribe", h.UnsubscribeFromIssue)
					r.Post("/unsubscribe/subtree", h.UnsubscribeFromIssueSubtree)
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Post("/rerun", h.RerunIssue)
					r.Post("/quick-actions/{quickActionId}/run", h.RunQuickAction)
					r.Post("/quick-actions/{quickActionId}/render", h.RenderQuickAction)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Get("/usage", h.GetIssueUsage)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
					r.Get("/children", h.ListChildIssues)
					r.Get("/labels", h.ListLabelsForIssue)
					r.Post("/labels", h.AttachLabel)
					r.Delete("/labels/{labelId}", h.DetachLabel)
					r.Get("/metadata", h.ListIssueMetadata)
					r.Put("/metadata/{key}", h.SetIssueMetadataKey)
					r.Delete("/metadata/{key}", h.DeleteIssueMetadataKey)
					r.Put("/properties/{propertyId}", h.SetIssueProperty)
					r.Delete("/properties/{propertyId}", h.DeleteIssueProperty)
					r.Get("/pull-requests", h.ListPullRequestsForIssue)
				})
			})

			// Task messages (user-facing, not daemon auth)
			r.Get("/api/tasks/{taskId}/messages", h.ListTaskMessagesByUser)
			r.With(handler.RequireHumanActor).Post("/api/tasks/{taskId}/retry-source-context", h.RetrySourceContextQuickCreate)

			// Issue quick actions (definitions; running one lives under
			// /api/issues/{id}/quick-actions/{quickActionId}/run)
			r.Route("/api/quick-actions", func(r chi.Router) {
				r.Get("/", h.ListQuickActions)
				r.Post("/", h.CreateQuickAction)
				r.Route("/{id}", func(r chi.Router) {
					r.Patch("/", h.UpdateQuickAction)
					r.Delete("/", h.DeleteQuickAction)
				})
			})

			// Custom issue properties (definitions; values live under /api/issues/{id}/properties)
			r.Route("/api/properties", func(r chi.Router) {
				r.Get("/", h.ListProperties)
				r.Post("/", h.CreateProperty)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProperty)
					r.Patch("/", h.UpdateProperty)
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

			// Issue status catalog (MUL-6243). Reads are open to any member —
			// every client needs the catalog to render a status. Writes are
			// gated to workspace owner/admin inside the handlers.
			r.Route("/api/issue-statuses", func(r chi.Router) {
				r.Get("/", h.ListIssueStatuses)
				r.Post("/", h.CreateIssueStatus)
				r.Patch("/reorder", h.ReorderIssueStatuses)
				r.Route("/{id}", func(r chi.Router) {
					r.Patch("/", h.UpdateIssueStatus)
					r.Delete("/", h.ArchiveIssueStatus)
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
					r.Get("/resources", h.ListProjectResources)
					r.Post("/resources", h.CreateProjectResource)
					r.Put("/resources/{resourceId}", h.UpdateProjectResource)
					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
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
				r.Get("/cron-preview", h.CronPreview)
				r.Get("/usage", h.GetAutopilotQuotaUsage)
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
					r.Post("/collaborators", h.AddAutopilotCollaborator)
					r.Delete("/collaborators/{userId}", h.RemoveAutopilotCollaborator)
				})
			})

			// Pins
			r.Route("/api/pins", func(r chi.Router) {
				r.Get("/", h.ListPins)
				r.Post("/", h.CreatePin)
				r.Put("/reorder", h.ReorderPins)
				r.Delete("/{itemType}/{itemId}", h.DeletePin)
			})

			// Saved issue views (MUL-4796).
			r.Get("/api/issue-view-preferences", h.GetIssueViewPreference)
			r.Put("/api/issue-view-preferences", h.PutIssueViewPreference)
			r.Route("/api/issue-views", func(r chi.Router) {
				r.Get("/", h.ListIssueViews)
				r.Post("/", h.CreateIssueView)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssueViewByID)
					r.Patch("/", h.UpdateIssueView)
					r.Delete("/", h.DeleteIssueView)
				})
			})

			// Attachments
			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			// /api/attachments/{id}/download is registered in the
			// outer Auth-only group above so it can be loaded as a
			// native <img>/<video> src without workspace headers
			// (MUL-3130). The handler self-resolves the workspace
			// from the attachment row.
			r.Get("/api/attachments/{id}/content", h.GetAttachmentContent)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			// Comments
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.With(handler.RequireHumanActor).Get("/sub-issue-preview", h.PreviewCommentSubIssue)
				r.With(handler.RequireHumanActor).Post("/sub-issues", h.CreateCommentSubIssue)
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
				// The workspace's built-in Chief of Staff. Server-owned: the
				// caller supplies only a runtime and a language, so a client
				// cannot mint an agent carrying `system_key` and thereby claim
				// the system instruction layer. Idempotent per workspace.
				r.Post("/mika", h.CreateMikaAgent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAgent)
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Post("/cancel-tasks", h.CancelAgentTasks)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/dingtalk/groups", h.ListDingTalkGroupsForAgent)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)
					r.Post("/skills/add", h.AddAgentSkills)
					r.Get("/labels", h.ListLabelsForAgent)
					r.Post("/labels", h.AttachLabelToAgent)
					r.Delete("/labels/{labelId}", h.DetachLabelFromAgent)
					r.Put("/skills/{skillId}/enabled", h.SetAgentSkillEnabled)
					r.Put("/runtime-skills/enabled", h.SetAgentRuntimeSkillEnabled)
					r.Delete("/skills/{skillId}", h.RemoveAgentSkill)
					// Workspace MCP servers assigned to this agent. Mirrors
					// the skills routes above: a library entry does nothing
					// until it is added here, and the binding carries its own
					// enabled toggle.
					r.Get("/mcp-servers", h.ListAgentMcpServers)
					r.Post("/mcp-servers", h.AddAgentMcpServer)
					r.Put("/mcp-servers/{serverId}/enabled", h.SetAgentMcpServerEnabled)
					r.Delete("/mcp-servers/{serverId}", h.RemoveAgentMcpServer)
					// Dedicated env-management endpoint. Admits the agent
					// owner or a workspace owner/admin; agent actors are
					// denied. Every reveal / write is audited to
					// activity_log. See MUL-2600, MUL-5438 and
					// internal/handler/agent_env.go.
					r.Get("/env", h.GetAgentEnv)
					r.Put("/env", h.UpdateAgentEnv)
				})
			})

			r.Route("/api/agent-builder/sessions", func(r chi.Router) {
				// The creation studio's unfinished drafts. Builder sessions are
				// invisible to every chat list (their carrier is kind='system'),
				// so this is the only route back to one.
				r.Get("/", h.ListAgentBuilderSessions)
				r.Post("/", h.CreateAgentBuilderSession)
				r.Patch("/{sessionId}/runtime", h.SwitchAgentBuilderRuntime)
				// Autosaved configuration, including edits the user has typed
				// but not sent. Read back through the list above.
				r.Put("/{sessionId}/draft", h.SaveAgentBuilderDraft)
			})

			// Skills
			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.Post("/", h.CreateSkill)
				r.Get("/search", h.SearchSkills)
				r.Post("/import", h.ImportSkill)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Post("/refresh", h.RefreshSkill)
					r.Get("/labels", h.ListLabelsForSkill)
					r.Post("/labels", h.AttachLabelToSkill)
					r.Delete("/labels/{labelId}", h.DetachLabelFromSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
				})
			})

			// Dashboard — workspace-wide token + run-time rollups for the
			// "/{slug}/dashboard" page. Optional ?project_id filter scopes
			// the rollup to a single project.
			r.Route("/api/dashboard", func(r chi.Router) {
				r.Get("/usage/daily", h.GetDashboardUsageDaily)
				r.Get("/usage/by-agent", h.GetDashboardUsageByAgent)
				r.Get("/agent-runtime", h.GetDashboardAgentRunTime)
				r.Get("/runtime/daily", h.GetDashboardRunTimeDaily)
				r.Get("/failures/daily", h.GetDashboardFailuresDaily)
				r.Get("/failures/by-agent", h.GetDashboardFailuresByAgent)
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
					r.Post("/models", h.InitiateListModels)
					r.Get("/models/{requestId}", h.GetModelListRequest)
					r.Post("/local-skills", h.InitiateListLocalSkills)
					r.Get("/local-skills/{requestId}", h.GetLocalSkillListRequest)
					r.Post("/local-skills/import", h.InitiateImportLocalSkill)
					r.Get("/local-skills/import/{requestId}", h.GetLocalSkillImportRequest)
					r.Delete("/", h.DeleteAgentRuntime)
					// Confirmed variant of DELETE: unbind every agent bound to
					// this runtime (they keep their configuration and chats and
					// need a new runtime to run again), cancel their tasks,
					// detach their task history, then delete the runtime — all
					// in one transaction. Used by the DeleteRuntimeDialog when
					// the strict DELETE refused with
					// `runtime_has_active_agents` and the user confirmed.
					r.Post("/unbind-agents-and-delete", h.UnbindAgentsAndDeleteRuntime)
					// Legacy path for installed clients built against the
					// archive-and-delete contract (MUL-5559 renamed the
					// behaviour, not just the route). Same handler.
					r.Post("/archive-agents-and-delete", h.UnbindAgentsAndDeleteRuntime)
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

			// Workspace-wide agent task snapshot for presence derivation:
			// every active task + each agent's most recent terminal task.
			r.Get("/api/agent-task-snapshot", h.ListWorkspaceAgentTaskSnapshot)

			// Independent workspace-level list backing the issues-header
			// "agents working" chip and its assignee-id Table filter.
			r.Get("/api/working-agents", h.ListWorkspaceWorkingAgents)

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
					r.Patch("/", h.UpdateChatSession)
					r.Patch("/pin", h.SetChatSessionPinned)
					r.Patch("/archive", h.SetChatSessionArchived)
					r.Delete("/", h.DeleteChatSession)
					r.Post("/messages", h.SendChatMessage)
					r.Post("/onboarding", h.StartMikaOnboarding)
					// Explicit "refresh" of a turn's quick actions: re-runs the
					// daemon suggestion pass for the latest assistant reply (MUL-5149).
					r.Post("/quick-actions/regenerate", h.RegenerateChatQuickActions)
					r.Get("/messages", h.ListChatMessages)
					r.Get("/messages/page", h.ListChatMessagesPage)
					r.Get("/pending-task", h.GetPendingChatTask)
					r.Delete("/queued-tasks", h.ClearQueuedChatTasks)
					r.Post("/queued-tasks/{taskId}/prioritize", h.PrioritizeQueuedChatTask)
					r.Post("/read", h.MarkChatSessionRead)
					// Deferred-cancellation draft restores (#5219):
					// creator-only fetch + idempotent consume.
					r.Get("/draft-restores", h.ListChatDraftRestores)
					r.Delete("/draft-restores/{restoreId}", h.ConsumeChatDraftRestore)
				})
			})
			r.Get("/api/chat/pending-tasks", h.ListPendingChatTasks)
			r.Get("/api/chat/pending-tasks/has-any", h.HasPendingChatTasks)

			// Quick-agent bar: per-user pinned agents for one-tap new chats.
			r.Get("/api/chat/pinned-agents", h.ListChatPinnedAgents)
			r.Post("/api/chat/pinned-agents", h.PinChatAgent)
			r.Delete("/api/chat/pinned-agents/{agentId}", h.UnpinChatAgent)

			// Agent-facing channel reads (MUL-3871). The caller's task-scoped token
			// resolves to its own chat session; no session/channel id is passed, so
			// an agent can only read its own conversation. `history` is the channel
			// overview (top-level messages + thread metadata); `thread` reads one
			// thread (?id for a specific one, else the thread the session is in).
			r.Get("/api/chat/history", h.GetChatChannelHistory)
			r.Get("/api/chat/thread", h.GetChatThread)

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				// Archived notifications, for the inbox's "Archived" sub-view.
				// Separate from "/" so the main list keeps its contract and
				// never carries the unbounded archive.
				r.Get("/archived", h.ListArchivedInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				// Cross-workspace unread summary: account-level, keyed on the
				// user. Backs the workspace-switcher dot for OTHER workspaces.
				r.Get("/unread-summary", h.UnreadInboxSummary)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/unread", h.MarkInboxUnread)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
				r.Post("/{id}/unarchive", h.UnarchiveInboxItem)
			})

			// Notification preferences
			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreferences)
				r.Patch("/", h.PatchNotificationPreferences)
				r.Put("/", h.UpdateNotificationPreferences)
			})
		})
	})

	return r, h
}

// buildLarkConnector wires the real WS long-conn connector that talks
// to /callback/ws/endpoint directly with app_id/app_secret. The
// connector wraps every read with a ctx-cancel watchdog so lease loss /
// shutdown breaks the blocking ReadMessage in bounded time — the
// invariant §4.4 leans on. A single connector instance serves every
// installation; its Run is parameterized by the installation, so the
// feishuChannel hands it the per-installation row.
//
// If the endpoint fetcher fails to initialize (typically a malformed
// MULTICA_LARK_CALLBACK_BASE_URL), we log and fall back to the
// NoopConnector so the lease / supervisor lifecycle still exercises
// against real DB rows. Inbound messages are silently dropped until
// the config is fixed; the boot log labels the mode "noop" so the
// degraded state is visible.
//
// Returns the connector plus a short label for the boot log:
// "ws-long-conn" in the healthy case, "noop" in the fallback case.
func buildLarkConnector(installSvc *lark.InstallationService, apiClient lark.APIClient) (lark.EventConnector, string) {
	endpointFetcher, err := lark.NewHTTPConnectionTokenFetcher(lark.HTTPConnectionTokenConfig{
		BaseURL: strings.TrimSpace(os.Getenv("MULTICA_LARK_CALLBACK_BASE_URL")),
		Logger:  slog.Default(),
	})
	if err != nil {
		slog.Error("lark ws: endpoint fetcher init failed; falling back to noop", "error", err)
		return lark.NewNoopConnector(slog.Default()), "noop"
	}
	decoder := lark.NewLarkJSONFrameDecoder()
	dialer := lark.NewGorillaDialer()
	if proxyURL := strings.TrimSpace(os.Getenv("MULTICA_LARK_WS_PROXY_URL")); proxyURL != "" {
		dialer.ProxyURL = proxyURL
	}
	credsProvider := lark.CredentialsProviderFunc(func(ctx context.Context, inst lark.Installation) (lark.InstallationCredentials, error) {
		secret, err := installSvc.DecryptAppSecret(inst)
		if err != nil {
			return lark.InstallationCredentials{}, err
		}
		creds := lark.InstallationCredentials{
			AppID:     inst.AppID,
			AppSecret: secret,
			Region:    lark.RegionOrDefault(inst.Region),
		}
		if inst.TenantKey.Valid {
			creds.TenantKey = inst.TenantKey.String
		}
		return creds, nil
	})
	// Inbound enricher: expands quoted replies / forwarded bundles AND
	// prefetches a window of surrounding group history (MUL-3084) into the
	// agent's body via the IM API before dispatch. It shares the
	// connector's resolved credentials and runs under the connector's
	// EnrichTimeout so it cannot overrun the Lark long-conn ACK budget.
	enricher := lark.NewInboundEnricher(apiClient, lark.InboundEnricherConfig{
		RecentContextSize: lark.DefaultRecentContextSize,
		Logger:            slog.Default(),
	})
	conn, err := lark.NewWSLongConnConnector(lark.WSConnectorConfig{
		Dialer:              dialer,
		EndpointFetcher:     endpointFetcher,
		FrameDecoder:        decoder,
		Enricher:            enricher,
		CredentialsProvider: credsProvider,
		Logger:              slog.Default(),
	})
	if err != nil {
		slog.Error("lark ws: connector init failed; falling back to noop", "error", err)
		return lark.NewNoopConnector(slog.Default()), "noop"
	}
	return conn, "ws-long-conn"
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

// composioStateSecret resolves the HMAC key for the connect-state. Prefers an
// explicit COMPOSIO_STATE_SECRET; otherwise derives a composio-specific key
// from JWT_SECRET via SHA-256 so the two signing domains never share an
// identical key. Returns nil when neither is set (composio stays disabled).
func composioStateSecret() []byte {
	if v := strings.TrimSpace(os.Getenv("COMPOSIO_STATE_SECRET")); v != "" {
		return []byte(v)
	}
	if v := strings.TrimSpace(os.Getenv("JWT_SECRET")); v != "" {
		sum := sha256.Sum256([]byte("composio-state:" + v))
		return sum[:]
	}
	return nil
}

// composioCallbackBaseURL resolves the public API base used to build the
// Composio callback URL. Prefers COMPOSIO_CALLBACK_BASE_URL, then the
// already-resolved MULTICA_PUBLIC_URL, then the app URL.
func composioCallbackBaseURL(publicURL string) string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("COMPOSIO_CALLBACK_BASE_URL")), "/"); v != "" {
		return v
	}
	if publicURL != "" {
		return publicURL
	}
	return appURLFromEnv()
}

// wecomMetricsOrNil keeps a typed nil out of the adapter's interface field.
// A *WecomMetrics that is nil still satisfies wecom.Metrics, so assigning it
// directly would give the adapter a non-nil interface holding a nil pointer —
// and the first counter call would panic on a deployment with /metrics off.
func wecomMetricsOrNil(m *obsmetrics.WecomMetrics) wecom.Metrics {
	if m == nil {
		return nil
	}
	return m
}
