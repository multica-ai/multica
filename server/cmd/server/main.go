package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/dbstartup"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/profiling"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/scheduler"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/redis/go-redis/v9"
)

var (
	version = "dev"
	commit  = "unknown"
)

func newNamedRedisClient(base *redis.Options, suffix string) *redis.Client {
	opts := *base
	if envBool("REDIS_DISABLE_CLIENT_NAME", false) {
		opts.ClientName = ""
	} else {
		opts.ClientName = redisClientName(opts.ClientName, suffix)
	}
	return redis.NewClient(&opts)
}

func redisClientName(existing, suffix string) string {
	if suffix == "" {
		return existing
	}
	if existing != "" {
		return existing + ":" + suffix
	}
	return "multica-api:" + suffix
}

func channelLeaseRedisURLFromEnv() string {
	if dedicated := strings.TrimSpace(os.Getenv("CHANNEL_WS_LEASE_REDIS_URL")); dedicated != "" {
		return dedicated
	}
	return strings.TrimSpace(os.Getenv("REDIS_URL"))
}

func realtimeRelayRedisURLFromEnv() string {
	if dedicated := strings.TrimSpace(os.Getenv("REALTIME_RELAY_REDIS_URL")); dedicated != "" {
		return dedicated
	}
	return strings.TrimSpace(os.Getenv("REDIS_URL"))
}

func closeRedisClient(label string, client *redis.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		slog.Warn("redis client close failed", "client", label, "error", err)
	}
}

func shardedRelayConfigFromEnv() realtime.ShardedStreamRelayConfig {
	cfg := realtime.DefaultShardedStreamRelayConfig()
	cfg.Shards = envPositiveInt("REALTIME_RELAY_SHARDS", cfg.Shards)
	cfg.StreamMaxLen = envPositiveInt64("REALTIME_RELAY_STREAM_MAXLEN", cfg.StreamMaxLen)
	cfg.ReadCount = envPositiveInt64("REALTIME_RELAY_XREAD_COUNT", cfg.ReadCount)
	cfg.ReadBlock = envDuration("REALTIME_RELAY_XREAD_BLOCK", cfg.ReadBlock)
	cfg.ReplayGrace = envDuration("REALTIME_RELAY_REPLAY_GRACE", cfg.ReplayGrace)
	cfg.TrimHorizon = envDuration("REALTIME_RELAY_TRIM_HORIZON", 2*cfg.ReplayGrace)
	cfg.StreamTTL = envDuration("REALTIME_RELAY_STREAM_TTL", cfg.TrimHorizon+cfg.ReplayGrace)
	cfg.TTLRefreshInterval = envDuration("REALTIME_RELAY_TTL_REFRESH_INTERVAL", cfg.TTLRefreshInterval)
	cfg.MaintenanceInterval = envDuration("REALTIME_RELAY_MAINTENANCE_INTERVAL", cfg.MaintenanceInterval)
	cfg.StreamTTLEnabled = envBool("REALTIME_RELAY_STREAM_TTL_ENABLED", false)
	if err := cfg.Validate(); err != nil {
		slog.Warn("invalid realtime relay retention config; normalizing to safe values", "error", err)
	}
	return cfg.Normalized()
}

func realtimeRelayModeFromEnv() string {
	const defaultMode = "sharded"
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("REALTIME_RELAY_MODE")))
	if raw == "" {
		return defaultMode
	}
	switch raw {
	case "sharded", "dual", "legacy":
		return raw
	default:
		slog.Warn("invalid env var, using default", "name", "REALTIME_RELAY_MODE", "value", raw, "default", defaultMode)
		return defaultMode
	}
}

func envPositiveInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func envNonNegativeInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

// maxLLMRetriesLimit caps MULTICA_LLM_MAX_RETRIES. The ceiling is a latency
// budget, not a taste call: SDK backoff is 0.5s doubling to an 8s cap, so 6
// retries spend ~21s and 10 spend ~48s sleeping before the last attempt. Every
// internal caller of pkg/llm runs under a far tighter deadline (8s for chat
// quick actions, 20s for title generation), so a budget past 5 cannot finish —
// it only converts a retryable upstream failure into a deadline-exceeded one.
const maxLLMRetriesLimit = 5

// parseLLMMaxRetries turns the raw MULTICA_LLM_MAX_RETRIES value into the
// tri-state llm.Config.MaxRetries expects: nil for unset (use the default),
// llm.Retries(0) to disable retries, llm.Retries(N) for a ceiling of N.
//
// Unlike the envFooInt helpers above it returns an error instead of warning and
// falling back to a default. A retry budget silently corrected to something the
// operator did not ask for is the failure this knob exists to remove
// (MUL-6364): a typo'd "3x" or a negative must stop the boot, not quietly
// restore the default and look configured.
func parseLLMMaxRetries(raw string) (*llm.RetryOverride, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("must be an integer, got %q", raw)
	}
	if v > maxLLMRetriesLimit {
		return nil, fmt.Errorf("must be at most %d, got %d", maxLLMRetriesLimit, v)
	}
	// llm.Retries owns the lower bound. It is the boundary that makes a negative
	// budget unrepresentable, and this is the only place the server builds one,
	// so the deployment-specific ceiling above and the type-level floor here
	// cannot disagree.
	override, err := llm.Retries(v)
	if err != nil {
		return nil, fmt.Errorf("must not be negative, got %d (use 0 to disable retries)", v)
	}
	return override, nil
}

func envPositiveInt64(name string, def int64) int64 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func envDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envNonNegativeDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func holdBeforeShutdown(sig os.Signal, signals <-chan os.Signal, duration time.Duration) {
	if duration <= 0 {
		return
	}
	slog.Info("termination signal received; holding before shutdown",
		"signal", sig.String(),
		"duration", duration.String(),
	)
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		slog.Info("shutdown hold complete", "duration", duration.String())
	case interruptSig := <-signals:
		slog.Info("shutdown hold interrupted by signal",
			"signal", interruptSig.String(),
			"configured_duration", duration.String(),
		)
	}
}

func envBool(name string, def bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func backgroundServices(h *handler.Handler) (*service.TaskService, *service.AutopilotService) {
	return h.TaskService, h.AutopilotService
}

// jwtSecretBootError returns a non-nil error when the combination of
// JWT_SECRET and APP_ENV is unsafe to boot with: production must never run
// on an empty or publicly-known default secret (auth.ValidateJWTSecret).
// Non-production keeps the historical dev fallback (see auth.JWTSecret)
// and only warns.
func jwtSecretBootError(jwtSecret, appEnv string) error {
	isProduction := strings.EqualFold(strings.TrimSpace(appEnv), "production")
	if !isProduction {
		return nil
	}
	return auth.ValidateJWTSecret(jwtSecret)
}

// newMainHTTPServer builds the public HTTP server with the production timeout
// defaults. These values are load-bearing safety settings, not cosmetic tuning,
// so they live in one helper that main() and the config regression test share.
//
// Bound header reads to stop Slowloris; IdleTimeout is looser than the
// metrics/profiling servers' 30s for keep-alive-heavy CLI and daemon clients.
// ReadTimeout and WriteTimeout are deliberately left zero so WebSocket upgrades
// (/ws, /api/daemon/ws) on this listener aren't killed mid-connection.
func newMainHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func main() {
	logger.Init()
	// Warn about missing configuration
	if err := jwtSecretBootError(os.Getenv("JWT_SECRET"), os.Getenv("APP_ENV")); err != nil {
		slog.Error(
			"refusing to start: "+err.Error()+
				"; generate a strong secret with `openssl rand -hex 32` and set JWT_SECRET (see .env.example)",
			"app_env", os.Getenv("APP_ENV"),
		)
		os.Exit(1)
	}
	if os.Getenv("JWT_SECRET") == "" {
		slog.Warn("JWT_SECRET is not set — using insecure dev default (allowed only because APP_ENV is not production).")
	}
	if os.Getenv("RESEND_API_KEY") == "" && strings.TrimSpace(os.Getenv("SMTP_HOST")) == "" {
		slog.Warn("no email backend configured (RESEND_API_KEY and SMTP_HOST both empty) — verification codes will be printed to the log instead of emailed.")
	}
	if os.Getenv("MULTICA_DEV_VERIFICATION_CODE") != "" {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
			slog.Warn("MULTICA_DEV_VERIFICATION_CODE is set but ignored because APP_ENV=production.")
		} else {
			slog.Warn("MULTICA_DEV_VERIFICATION_CODE is enabled. Use it only for local development or private test instances.")
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	shutdownHoldDuration := envNonNegativeDuration("MULTICA_SHUTDOWN_HOLD_DURATION", 0)

	// Feature flags: loaded once at startup from MULTICA_FEATURE_FLAGS_FILE
	// (a YAML rule set) with FF_<KEY> env overrides layered on top.
	// See server/pkg/featureflag for the schema and lifecycle rules.
	//
	// Booting the server without any flag config is intentional: when the
	// env var is unset, every IsEnabled call falls through to the caller's
	// default, so existing code paths are unchanged until someone adds a
	// rule. A misconfigured (malformed / missing) file surfaces as a hard
	// error so operators see misconfig the same way they do for any other
	// MULTICA_*_FILE knob.
	flags, err := featureflag.NewServiceFromEnv(featureflag.WithLogger(slog.Default()))
	if err != nil {
		slog.Error("feature flag configuration failed to load", "error", err)
		os.Exit(1)
	}
	_ = flags // adopted by the router (opts.FeatureFlags) and server-side toggle points; see server/pkg/featureflag

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	startupSettings := dbstartup.SettingsFromEnv()
	pool, err := newDBPool(context.Background(), dbURL, startupSettings.ConnectTimeout)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	startupCtx, stopStartup := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	retryOptions := startupSettings.RetryOptions()
	retryOptions.ShouldRetry = dbstartup.IsTransientDatabaseError
	retryOptions.OnRetry = func(event dbstartup.RetryEvent) {
		slog.Warn("database unavailable during server startup; retrying",
			"attempt", event.Attempt,
			"retry_in", event.Delay,
			"error", event.Err,
		)
	}
	if err := dbstartup.Retry(startupCtx, retryOptions, pool.Ping); err != nil {
		stopStartup()
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	stopStartup()
	slog.Info("connected to database")
	logPoolConfig(pool)

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	daemonHub := daemonws.NewHub()
	var daemonWakeup interface {
		service.TaskWakeupNotifier
		handler.RuntimeGoneNotifier
	} = daemonHub
	// Nil unless a Redis relay is running: without one there is only one
	// replica, and it both publishes the completion and holds the socket.
	var wecomRelay WecomRelay
	// Built here rather than in the router because the relay's shard readers
	// start before the router exists, and a deliverer registered after they
	// start would miss the replay window they open with. The registry is a
	// bare map with no dependencies, so it can be minted this early and handed
	// down.
	wecomSenders := wecom.NewSendersRegistry()
	var wecomRelayOutbound *wecom.RelayOutbound

	// MUL-1138: when REDIS_URL is set, route fanout through a Redis relay so
	// multiple API nodes can deliver each other's events. Without it the hub
	// is the sole broadcaster and the server stays single-node (legacy).
	// Runtime local-skill stores and realtime relay traffic use separate Redis
	// clients so blocking stream consumers cannot starve request-path Redis
	// operations. Channel leases are initialized separately below so production
	// can point them at a dedicated no-eviction Redis instance.
	relayCtx, relayCancel := context.WithCancel(context.Background())
	var broadcaster realtime.Broadcaster = hub
	var storeRedis *redis.Client
	var channelLeaseRedis *redis.Client
	var relayWriteRedis *redis.Client
	var relayReadRedis *redis.Client
	var shardedReadRedis *redis.Client
	var legacyReadRedis *redis.Client
	var relay realtime.ManagedRelay
	// stopRelay halts the relay readers and drains the WeCom dispatcher. It is
	// called from the shutdown BODY, before the channel supervisor is torn
	// down — the dispatcher's drain sends over the sockets the supervisor
	// owns, and each supervised connection clears its sender on exit, so a
	// drain that runs after that teardown finds ownsSocket false for every
	// installation and discards everything it was built to save. The defer
	// keeps a second call (idempotent via the Once) for the early-return
	// paths that never reach the body's shutdown sequence.
	var stopRelayOnce sync.Once
	stopRelay := func() {
		stopRelayOnce.Do(func() {
			if relay != nil {
				relay.Stop()
			}
			relayCancel()
			if relay != nil {
				relay.Wait()
			}
			// Join the dispatcher BEFORE its Redis clients go away, or the
			// drain races the teardown it depends on.
			wecomRelayOutbound.Wait()
		})
	}
	defer func() {
		stopRelay()
		closeRedisClient("realtime-read-legacy", legacyReadRedis)
		closeRedisClient("realtime-read-sharded", shardedReadRedis)
		closeRedisClient("realtime-read", relayReadRedis)
		closeRedisClient("realtime-write", relayWriteRedis)
		closeRedisClient("channel-lease", channelLeaseRedis)
		closeRedisClient("store", storeRedis)
	}()
	sharedRedisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	relayRedisURL := realtimeRelayRedisURLFromEnv()
	if (sharedRedisURL != "" || relayRedisURL != "") && envBool("REDIS_DISABLE_CLIENT_NAME", false) {
		slog.Info("redis: CLIENT SETNAME disabled (REDIS_DISABLE_CLIENT_NAME=true) for managed Redis compatibility")
	}
	if sharedRedisURL != "" {
		if opts, err := redis.ParseURL(sharedRedisURL); err != nil {
			slog.Error("invalid REDIS_URL — request-path Redis features disabled", "error", err)
		} else {
			storeRedis = newNamedRedisClient(opts, "store")
		}
	}
	if relayRedisURL != "" {
		opts, err := redis.ParseURL(relayRedisURL)
		if err != nil {
			slog.Error("invalid realtime relay Redis URL — falling back to in-memory hub", "error", err)
		} else {
			relayWriteRedis = newNamedRedisClient(opts, "realtime-write")

			relayMode := realtimeRelayModeFromEnv()
			relayConfig := shardedRelayConfigFromEnv()
			switch relayMode {
			case "legacy":
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				relay = realtime.NewRedisRelayWithClientsAndConfig(hub, relayWriteRedis, relayReadRedis, relayConfig.RetentionConfig())
				slog.Info("daemon websocket wakeup: Redis fanout disabled in legacy realtime relay mode")
			case "dual":
				shardedReadRedis = newNamedRedisClient(opts, "realtime-read-sharded")
				legacyReadRedis = newNamedRedisClient(opts, "realtime-read-legacy")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, shardedReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				wecomRelay = sharded
				legacy := realtime.NewRedisRelayWithClientsAndConfig(hub, relayWriteRedis, legacyReadRedis, relayConfig.RetentionConfig())
				relay = realtime.NewMirroredRelay(sharded, legacy)
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			default:
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, relayReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				wecomRelay = sharded
				relay = sharded
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			}
			if wecomRelay != nil {
				// LeaseSettle comes from the supervisor's own knob rather than
				// from a constant here: the re-offer chain exists to outlast a
				// lease move, and how long that takes is the supervisor's poll
				// interval, which a deployment can tune.
				leaseSettle, err := channelLeasePollInterval()
				if err != nil {
					slog.Warn("wecom relay: unreadable lease poll interval, using the dispatcher default",
						"error", err)
					leaseSettle = 0
				}
				wecomRelayOutbound = wecom.NewRelayOutbound(wecomRelay,
					wecom.NewRedisDedupe(relayWriteRedis, 0, slog.Default()),
					wecom.RelayConfig{
						ReplayGrace: relayConfig.ReplayGrace,
						LeaseSettle: leaseSettle,
					}, slog.Default())
				wecomRelay.SetWecomOutboundDeliverer(wecomRelayOutbound)
				wecomRelayOutbound.Start(relayCtx)
			}
			// Every deliverer is registered by this point. The shard readers
			// open on a replay window, so anything registered after Start
			// silently misses it.
			relay.Start(relayCtx)
			broadcaster = realtime.NewDualWriteBroadcaster(hub, relay)
			storePoolSize := 0
			if storeRedis != nil {
				storePoolSize = storeRedis.Options().PoolSize
			}
			slog.Info(
				"realtime: Redis relay enabled",
				"node_id", relay.NodeID(),
				"mode", relayMode,
				"dedicated_instance", strings.TrimSpace(os.Getenv("REALTIME_RELAY_REDIS_URL")) != "",
				"shards", relayConfig.Shards,
				"stream_max_len", relayConfig.StreamMaxLen,
				"replay_grace", relayConfig.ReplayGrace.String(),
				"trim_horizon", relayConfig.TrimHorizon.String(),
				"stream_ttl", relayConfig.StreamTTL.String(),
				"stream_ttl_enabled", relayConfig.StreamTTLEnabled,
				"xread_count", relayConfig.ReadCount,
				"xread_block", relayConfig.ReadBlock.String(),
				"store_pool_size", storePoolSize,
				"realtime_write_pool_size", opts.PoolSize,
				"realtime_read_pool_size", opts.PoolSize,
			)
		}
	} else {
		slog.Info("realtime: REDIS_URL and REALTIME_RELAY_REDIS_URL are unset — using in-memory hub (single-node mode)")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CHANNEL_WS_LEASE_BACKEND")), "redis") {
		leaseRedisURL := channelLeaseRedisURLFromEnv()
		if leaseRedisURL == "" {
			slog.Error("channel leases: CHANNEL_WS_LEASE_REDIS_URL and REDIS_URL are unset")
		} else if opts, err := redis.ParseURL(leaseRedisURL); err != nil {
			slog.Error("channel leases: invalid Redis URL; supervisor will fail closed", "error", err)
		} else {
			channelLeaseRedis = newNamedRedisClient(opts, "channel-lease")
		}
	}
	registerListeners(bus, broadcaster)

	analyticsClient := analytics.NewFromEnv()
	defer analyticsClient.Close()

	queries := db.New(pool)
	hub.SetAuthorizer(newScopeAuthorizer(queries))
	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, pool)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	metricsConfig := obsmetrics.ConfigFromEnv()
	var metricsServer *http.Server
	var httpMetrics *obsmetrics.HTTPMetrics
	var businessMetrics *obsmetrics.BusinessMetrics
	var channelMediaMetrics *obsmetrics.ChannelMediaReconcilerMetrics
	var channelLeaseMetrics *obsmetrics.ChannelLeaseMetrics
	var wecomMetrics *obsmetrics.WecomMetrics
	if metricsConfig.Enabled() {
		metricsRegistry := obsmetrics.NewRegistry(obsmetrics.RegistryOptions{
			Pool:     pool,
			Realtime: realtime.M,
			DaemonWS: daemonws.M,
			Version:  version,
			Commit:   commit,
		})
		httpMetrics = metricsRegistry.HTTP
		businessMetrics = metricsRegistry.Business
		channelMediaMetrics = metricsRegistry.ChannelMedia
		channelLeaseMetrics = metricsRegistry.ChannelLease
		wecomMetrics = metricsRegistry.Wecom
		// Forward inbound daemon WS frames into the per-kind counter so
		// dashboards can split heartbeat / unknown / invalid traffic.
		if daemonHub != nil {
			daemonHub.SetMessageKindRecorder(businessMetrics)
		}
		metricsServer = obsmetrics.NewServer(metricsConfig.Addr, metricsRegistry.Gatherer)
		if !obsmetrics.IsLoopbackAddr(metricsConfig.Addr) {
			slog.Warn(
				"metrics listener is not loopback-only; restrict access with private networking, allowlists, or proxy auth",
				"addr", metricsConfig.Addr,
			)
		}
	}
	// Construct the BatchedHeartbeatScheduler before the router so it can
	// be injected into the Handler. The Run goroutine starts below
	// alongside the sweeper, and Stop is called explicitly during graceful
	// shutdown so any pending bumps are flushed before we exit.
	heartbeatScheduler := handler.NewBatchedHeartbeatScheduler(queries, handler.DefaultHeartbeatBatchInterval, daemonWakeup)

	// Validate the LLM retry budget before the router exists: an operator who
	// typed a value we cannot honor should see the boot stop, the same way a
	// malformed feature-flag file does above.
	llmMaxRetries, err := parseLLMMaxRetries(os.Getenv("MULTICA_LLM_MAX_RETRIES"))
	if err != nil {
		slog.Error("invalid MULTICA_LLM_MAX_RETRIES", "error", err)
		os.Exit(1)
	}

	r, h := NewRouterWithOptions(pool, hub, bus, analyticsClient, storeRedis, RouterOptions{
		HTTPMetrics:         httpMetrics,
		BusinessMetrics:     businessMetrics,
		ChannelLeaseMetrics: channelLeaseMetrics,
		ChannelLeaseRedis:   channelLeaseRedis,
		WecomMetrics:        wecomMetrics,
		DaemonHub:           daemonHub,
		DaemonWakeup:        daemonWakeup,
		WecomSenders:        wecomSenders,
		WecomRelayOutbound:  wecomRelayOutbound,
		FeatureFlags:        flags,
		HeartbeatScheduler:  heartbeatScheduler,
		LLMMaxRetries:       llmMaxRetries,
	})

	srv := newMainHTTPServer(":"+port, r)
	profilingServer := profiling.NewServer()

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	// Reuse the router's services here. In particular, the router wires the
	// EmptyClaim cache into TaskService; constructing a second TaskService for
	// scheduled Autopilot dispatch would send the daemon wakeup without bumping
	// that cache's version, so an idle runtime could keep returning an empty
	// claim until the cache TTL expires.
	taskSvc, autopilotSvc := backgroundServices(h)
	registerAutopilotListeners(bus, autopilotSvc)

	// Construct a LivenessStore that mirrors the one wired into the HTTP
	// handler. Both the heartbeat write path (handler) and the sweeper read
	// path (here) must agree on the same Redis-or-Noop choice; if they
	// disagree, online runtimes get falsely marked offline.
	var liveness handler.LivenessStore = handler.NewNoopLivenessStore()
	if storeRedis != nil {
		liveness = handler.NewRedisLivenessStore(storeRedis)
	}

	// Start background sweeper to mark stale runtimes as offline.
	runtimeReconnectGrace := envDuration("MULTICA_RUNTIME_RECONNECT_GRACE", defaultRuntimeReconnectGrace)
	if runtimeReconnectGrace < minimumRuntimeReconnectGrace {
		slog.Warn("runtime reconnect grace is shorter than heartbeat freshness; clamping",
			"configured", runtimeReconnectGrace,
			"minimum", minimumRuntimeReconnectGrace,
		)
		runtimeReconnectGrace = minimumRuntimeReconnectGrace
	}
	// Queued work now expires on the same runtime-liveness signal as in-flight
	// work, so there is no separate queue TTL to tune: a busy runtime keeps its
	// backlog, and a departed one retires everything it owned at once.
	go runRuntimeSweeper(sweepCtx, queries, liveness, taskSvc, bus, runtimeReconnectGrace)
	go runDelegatedFailureRecoverySweeper(sweepCtx, taskSvc)
	// Seven-day runtime retention does not share the 30-second liveness tick:
	// its bounded transactions run independently once per hour, so a slow GC
	// round cannot delay offline detection or task recovery.
	go runRuntimeGCSweeper(sweepCtx, pool, queries, taskSvc.Metrics, h)
	// Source-context cleanup is object-store work, so it gets its own goroutine
	// instead of a slot in the runtime sweep tick.
	go runSourceContextSweeper(sweepCtx, taskSvc)
	go heartbeatScheduler.Run(sweepCtx)
	go runAutopilotFailureMonitor(autopilotCtx, queries, bus, envFailureMonitorConfig())
	if autopilotSvc.QuotaEnabled() {
		go runAutopilotQuotaReconciler(autopilotCtx, autopilotSvc)
	}
	go runDBStatsLogger(sweepCtx, pool)
	if h.WebhookDeliveryWorker != nil {
		go h.WebhookDeliveryWorker.Run(sweepCtx)
	}
	if h.SeatCapacityWorker != nil {
		go h.SeatCapacityWorker.Run(sweepCtx)
	}
	if h.TelegramOutbound != nil {
		h.TelegramOutbound.Start(sweepCtx)
	}
	// GitHub PR-card API snapshot pipeline (MUL-5265): worker pool + TTL sweeper.
	// No-op when unconfigured (no App private key).
	h.PRRefresh.Start(sweepCtx)

	// Channel inbound supervisor (MUL-3620): holds the §4.4 WS lease per
	// installation and drives each channel.Channel. It is channel-agnostic,
	// not Lark-specific, but remains nil when lease startup validation fails
	// (notably Redis fail-closed readiness). With no platform registered or no
	// installation rows it simply idles. Lifecycle is bound to sweepCtx so it winds down
	// alongside the other long-running workers, AFTER the HTTP server has
	// drained.
	if h.ChannelSupervisor != nil {
		go h.ChannelSupervisor.Run(sweepCtx)
	}

	// Media intent-ledger reconciler (PR #5580): settles uploaded-but-unbound
	// channel media objects. An independent worker so object-storage latency
	// spikes cannot starve any other sweeper's cadence.
	if h.ChannelMediaReconciler != nil {
		h.ChannelMediaReconciler.Metrics = channelMediaMetrics
		go h.ChannelMediaReconciler.Run(sweepCtx)
	}

	// MUL-2957: DB-backed execution scheduler. The scheduler turns the
	// `sys_cron_executions` table into the distributed lease + audit
	// log for internal periodic jobs. The first job is
	// `rollup_task_usage_hourly`, which replaces the previously
	// operator-registered `pg_cron` entry (still safe to run
	// concurrently — the SQL function holds advisory lock 4246).
	//
	// A failure to register the job is treated as fatal here only at
	// the registration step (a duplicate name is the only realistic
	// cause and indicates a code bug). Once running, the manager
	// surfaces transient errors — DB unreachable, sys_cron_executions
	// missing because of an unusual partial-migration state — by
	// logging them on the tick that fails and retrying on the next
	// cycle, so a temporary outage does not crash the server.
	schedulerMgr := scheduler.NewManager(pool, scheduler.Options{})
	if err := schedulerMgr.Register(scheduler.TaskUsageHourlyJob(pool)); err != nil {
		slog.Warn("scheduler: failed to register task_usage_hourly rollup job", "error", err)
	}
	// MUL-3551: scheduled-Autopilot dispatch runs on the same DB-backed
	// scheduler. The job owns its plan_times via PlansForScope (each
	// trigger has its own cron expression, so the Cadence planner does
	// not fit). Crash recovery, occurrence-level idempotency, lease
	// theft, and retry are all reused from the manager + sys_cron_executions
	// — there is no separate goroutine for scheduled Autopilot anymore.
	if err := schedulerMgr.Register(scheduler.AutopilotScheduleDispatchJob(pool, queries, autopilotSvc)); err != nil {
		slog.Warn("scheduler: failed to register autopilot_schedule_dispatch job", "error", err)
	}
	// Manifest-declared Plugin schedules share the same durable lease and retry
	// machinery. The job is inert while plugins_v1 is disabled.
	if err := schedulerMgr.Register(scheduler.PluginHookScheduleDispatchJob(queries, h.PluginService)); err != nil {
		slog.Warn("scheduler: failed to register plugin_hook_schedule_dispatch job", "error", err)
	}
	go func() {
		_ = schedulerMgr.Run(sweepCtx)
	}()

	if metricsServer != nil {
		go func() {
			slog.Info("metrics server starting", "addr", metricsConfig.Addr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server disabled after startup error", "error", err)
			}
		}()
	}

	go func() {
		slog.Info("pprof server starting", "addr", profilingServer.Addr)
		if err := profilingServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("pprof server disabled after startup error", "error", err)
		}
	}()

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	holdBeforeShutdown(sig, quit, shutdownHoldDuration)
	// Restore the default behavior so another signal during graceful shutdown
	// can still terminate the process instead of being left unread in quit.
	signal.Stop(quit)

	slog.Info("shutting down server")

	// The order below is the contract, and shutdown.go is where it is stated
	// and pinned. The one that is not self-evident: the WeCom dispatcher must
	// be drained BEFORE sweepCancel, not merely before the supervisor is
	// joined — cancelling the sweeper context is already what makes every
	// supervised connection clear its sender, and a drain past that point
	// finds no socket to deliver over.
	shutdownSequence{
		StopAutopilot: autopilotCancel,
		DrainHTTP: func() {
			apiShutdownCtx, apiShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := srv.Shutdown(apiShutdownCtx); err != nil {
				apiShutdownCancel()
				slog.Error("server forced to shutdown", "error", err)
				os.Exit(1)
			}
			apiShutdownCancel()
		},
		StopOutboundRelay: stopRelay,
		CancelWorkers:     sweepCancel,
		StopHeartbeats:    heartbeatScheduler.Stop,
		JoinWebhookWorker: func() {
			if h.WebhookDeliveryWorker != nil && !h.WebhookDeliveryWorker.WaitWithTimeout(5*time.Second) {
				slog.Warn("webhook delivery worker did not exit within shutdown timeout")
			}
		},
		JoinTelegram: func() {
			if h.TelegramOutbound != nil && !h.TelegramOutbound.WaitWithTimeout(5*time.Second) {
				slog.Warn("telegram outbound workers did not exit within shutdown timeout")
			}
		},
		// Joined so the lease renewer can issue a final release before exit;
		// otherwise the next replica waits out the whole LeaseTTL on the far
		// side of a redeploy. Bounded: a wedged supervisor falls back to the
		// natural expiry rather than holding shutdown open.
		JoinChannelSupervisor: func() {
			if h.ChannelSupervisor == nil {
				return
			}
			if !h.ChannelSupervisor.WaitWithTimeout(h.ChannelSupervisor.ShutdownTimeout()) {
				slog.Warn("channel supervisor: connections did not exit within shutdown timeout; proceeding",
					"timeout", h.ChannelSupervisor.ShutdownTimeout().String(),
				)
			}
		},
		DrainChannelRouter: func() {
			if h.ChannelSupervisor == nil || h.ChannelRouter == nil {
				return
			}
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if !h.ChannelRouter.Drain(drainCtx) {
				slog.Warn("channel router: drain deadline reached; deferred media fallback remains durable")
			}
			drainCancel()
		},
		StopMetricsServer: func() {
			if metricsServer == nil {
				return
			}
			metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
				slog.Error("metrics server forced to shutdown", "error", err)
			}
			metricsShutdownCancel()
		},
		StopProfiling: func() {
			profilingShutdownCtx, profilingShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := profilingServer.Shutdown(profilingShutdownCtx); err != nil {
				slog.Error("pprof server forced to shutdown", "error", err)
			}
			profilingShutdownCancel()
		},
	}.run()
	slog.Info("server stopped")
}
