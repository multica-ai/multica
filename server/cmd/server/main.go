package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/joho/godotenv"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/redis/go-redis/v9"
)

var (
	version = "dev"
	commit  = "unknown"
)

func newNamedRedisClient(base *redis.Options, suffix string) *redis.Client {
	opts := *base
	opts.ClientName = redisClientName(opts.ClientName, suffix)
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
	return cfg
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

func main() {
	// Load .env from project root or current directory.
	_ = godotenv.Load("../.env")
	_ = godotenv.Load()
	logger.Init()

	// Warn about missing configuration
	if os.Getenv("JWT_SECRET") == "" {
		slog.Warn("JWT_SECRET is not set — using insecure default. Set JWT_SECRET for production use.")
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
		port = "8081"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://costrict:costrict_password@localhost:5432/costrict"
	}

	// Connect to database
	ctx := context.Background()
	pool, err := newDBPool(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")
	logPoolConfig(pool)

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	daemonHub := daemonws.NewHub()
	var daemonWakeup service.TaskWakeupNotifier = daemonHub

	// MUL-1138: when REDIS_URL is set, route fanout through a Redis relay so
	// multiple API nodes can deliver each other's events. Without it the hub
	// is the sole broadcaster and the server stays single-node (legacy).
	// Runtime local-skill stores and realtime relay traffic use separate Redis
	// clients so blocking stream consumers cannot starve request-path Redis
	// operations.
	relayCtx, relayCancel := context.WithCancel(context.Background())
	var broadcaster realtime.Broadcaster = hub
	var storeRedis *redis.Client
	var relayWriteRedis *redis.Client
	var relayReadRedis *redis.Client
	var shardedReadRedis *redis.Client
	var legacyReadRedis *redis.Client
	var relay realtime.ManagedRelay
	defer func() {
		if relay != nil {
			relay.Stop()
		}
		relayCancel()
		if relay != nil {
			relay.Wait()
		}
		closeRedisClient("realtime-read-legacy", legacyReadRedis)
		closeRedisClient("realtime-read-sharded", shardedReadRedis)
		closeRedisClient("realtime-read", relayReadRedis)
		closeRedisClient("realtime-write", relayWriteRedis)
		closeRedisClient("store", storeRedis)
	}()
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			slog.Error("invalid REDIS_URL — falling back to in-memory hub", "error", err)
		} else {
			storeRedis = newNamedRedisClient(opts, "store")
			relayWriteRedis = newNamedRedisClient(opts, "realtime-write")

			relayMode := realtimeRelayModeFromEnv()
			relayConfig := shardedRelayConfigFromEnv()
			switch relayMode {
			case "legacy":
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				relay = realtime.NewRedisRelayWithClients(hub, relayWriteRedis, relayReadRedis)
				slog.Info("daemon websocket wakeup: Redis fanout disabled in legacy realtime relay mode")
			case "dual":
				shardedReadRedis = newNamedRedisClient(opts, "realtime-read-sharded")
				legacyReadRedis = newNamedRedisClient(opts, "realtime-read-legacy")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, shardedReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				legacy := realtime.NewRedisRelayWithClients(hub, relayWriteRedis, legacyReadRedis)
				relay = realtime.NewMirroredRelay(sharded, legacy)
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			default:
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, relayReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				relay = sharded
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			}
			relay.Start(relayCtx)
			broadcaster = realtime.NewDualWriteBroadcaster(hub, relay)
			slog.Info(
				"realtime: Redis relay enabled",
				"node_id", relay.NodeID(),
				"mode", relayMode,
				"shards", relayConfig.Shards,
				"stream_max_len", relayConfig.StreamMaxLen,
				"xread_count", relayConfig.ReadCount,
				"xread_block", relayConfig.ReadBlock.String(),
				"store_pool_size", opts.PoolSize,
				"realtime_write_pool_size", opts.PoolSize,
				"realtime_read_pool_size", opts.PoolSize,
			)
		}
	} else {
		slog.Info("realtime: REDIS_URL not set — using in-memory hub (single-node mode)")
	}
	registerListeners(bus, broadcaster)

	analyticsClient := analytics.NewFromEnv()
	defer analyticsClient.Close()

	queries := db.New(pool)
	hub.SetAuthorizer(newScopeAuthorizer(queries))
	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	metricsConfig := obsmetrics.ConfigFromEnv()
	var metricsServer *http.Server
	var httpMetrics *obsmetrics.HTTPMetrics
	if metricsConfig.Enabled() {
		metricsRegistry := obsmetrics.NewRegistry(obsmetrics.RegistryOptions{
			Pool:     pool,
			Realtime: realtime.M,
			DaemonWS: daemonws.M,
			Version:  version,
			Commit:   commit,
		})
		httpMetrics = metricsRegistry.HTTP
		metricsServer = obsmetrics.NewServer(metricsConfig.Addr, metricsRegistry.Gatherer)
		if !obsmetrics.IsLoopbackAddr(metricsConfig.Addr) {
			slog.Warn(
				"metrics listener is not loopback-only; restrict access with private networking, allowlists, or proxy auth",
				"addr", metricsConfig.Addr,
			)
		}
	}

	// Casdoor SSO: when CASDOOR_ENDPOINT is set, validate RS256 JWTs issued
	// by Casdoor. Otherwise fall back to legacy HMAC JWT auth. Both modes
	// support PAT tokens (the CasdoorAuth middleware passes "mul_" prefixed
	// Bearer tokens through to the downstream Auth middleware).
	casdoorEndpoint := os.Getenv("CASDOOR_ENDPOINT")
	casdoorEnabled := casdoorEndpoint != ""
	var jwksProvider *auth.JWKSProvider
	if casdoorEnabled {
		jwksProvider = auth.NewJWKSProvider(casdoorEndpoint)
		jwksProvider.Preload()
		slog.Info("Casdoor SSO enabled", "endpoint", casdoorEndpoint)
	} else {
		slog.Warn("Casdoor SSO not configured — using legacy HMAC JWT auth")
	}

	// subjectResolver maps a Casdoor subject_id (the "sub" claim) to a
	// Multica user UUID. On first encounter the user is auto-provisioned
	// with the real name/email from the JWT claims. For existing users,
	// the name and email are kept in sync with Casdoor.
	subjectResolver := middleware.SubjectResolver(func(ctx context.Context, subjectID, name, email string) (string, error) {
		user, err := queries.GetUserBySubjectID(ctx, pgtype.Text{String: subjectID, Valid: true})
		if err != nil {
			// Auto-provision: use real name/email from JWT, fall back to placeholders.
			if name == "" {
				name = "casdoor-" + subjectID
			}
			if email == "" {
				email = subjectID + "@casdoor.local"
			}
			user, err = queries.CreateUser(ctx, db.CreateUserParams{
				Name:      name,
				Email:     email,
				AvatarUrl: pgtype.Text{},
			})
			if err != nil {
				// The email already belongs to an existing account that isn't
				// linked to this subject_id yet — e.g. the person was
				// provisioned earlier under a different Casdoor subject (re-created
				// in Casdoor, org migration) or a pre-Casdoor local account holds
				// this email. Adopt that account by binding this subject_id to it,
				// unless it already carries a *different* subject_id (a genuine
				// two-identity-one-email collision we must not silently hijack).
				if util.IsUniqueViolation(err) {
					existing, findErr := queries.GetUserByEmail(ctx, email)
					if findErr == nil {
						if existing.SubjectID.Valid && existing.SubjectID.String != subjectID {
							slog.Warn("casdoor: email owned by a different subject_id, refusing to adopt",
								"subject_id", subjectID,
								"existing_subject_id", existing.SubjectID.String,
								"existing_user_id", util.UUIDToString(existing.ID),
								"email", email,
							)
							return "", err
						}
						if !existing.SubjectID.Valid {
							if setErr := queries.SetUserSubjectID(ctx, db.SetUserSubjectIDParams{
								ID:        existing.ID,
								SubjectID: pgtype.Text{String: subjectID, Valid: true},
							}); setErr != nil {
								slog.Warn("casdoor: failed to adopt existing user by email",
									"user_id", util.UUIDToString(existing.ID),
									"subject_id", subjectID,
									"error", setErr,
								)
								return "", setErr
							}
						}
						slog.Info("casdoor: adopted existing user by email",
							"user_id", util.UUIDToString(existing.ID),
							"subject_id", subjectID,
							"email", email,
						)
						return util.UUIDToString(existing.ID), nil
					}
				}
				return "", err
			}
			if setErr := queries.SetUserSubjectID(ctx, db.SetUserSubjectIDParams{
				ID:        user.ID,
				SubjectID: pgtype.Text{String: subjectID, Valid: true},
			}); setErr != nil {
				slog.Warn("failed to bind subject_id to auto-provisioned user",
					"user_id", util.UUIDToString(user.ID),
					"subject_id", subjectID,
					"error", setErr,
				)
			}
			slog.Info("casdoor: auto-provisioned user", "user_id", util.UUIDToString(user.ID), "subject_id", subjectID, "name", name)
			return util.UUIDToString(user.ID), nil
		}

		// Existing user: sync name/email if they changed in Casdoor.
		syncName := name != "" && user.Name != name
		syncEmail := email != "" && user.Email != email

		// Guard against unique-key violations: if another user already owns
		// this email (e.g. a pre-existing local account), skip the email sync.
		if syncEmail {
			existing, err := queries.GetUserByEmail(ctx, email)
			if err == nil && existing.ID != user.ID {
				slog.Warn("casdoor email already owned by another user, skipping email sync",
					"user_id", util.UUIDToString(user.ID),
					"existing_user_id", util.UUIDToString(existing.ID),
					"email", email,
				)
				syncEmail = false
			}
		}

		if syncName || syncEmail {
			if _, updErr := queries.UpdateUserNameAndEmail(ctx, db.UpdateUserNameAndEmailParams{
				ID:    user.ID,
				Name:  name,
				Email: email,
			}); updErr != nil {
				slog.Warn("failed to sync user profile from Casdoor",
					"user_id", util.UUIDToString(user.ID),
					"subject_id", subjectID,
					"error", updErr,
				)
			} else {
				slog.Info("casdoor: synced user profile", "user_id", util.UUIDToString(user.ID), "subject_id", subjectID, "name", name, "email_synced", syncEmail)
			}
		}
		return util.UUIDToString(user.ID), nil
	})

	// Construct the BatchedHeartbeatScheduler before the router so it can
	// be injected into the Handler. The Run goroutine starts below
	// alongside the sweeper, and Stop is called explicitly during graceful
	// shutdown so any pending bumps are flushed before we exit.
	heartbeatScheduler := handler.NewBatchedHeartbeatScheduler(queries, handler.DefaultHeartbeatBatchInterval)

	// Skill proxy: when COSTRICT_API_INTERNAL is set, create a proxy client
	// that forwards skill requests to the costrict-web internal API with
	// rate limiting, caching, and audit logging.
	var skillProxy *service.SkillProxy
	if costrictAPI := strings.TrimSpace(os.Getenv("COSTRICT_API_INTERNAL")); costrictAPI != "" {
		costrictSecret := os.Getenv("COSTRICT_INTERNAL_SECRET")
		skillProxy = service.NewSkillProxy(costrictAPI, costrictSecret, 5*time.Minute, queries)
		slog.Info("skill proxy enabled", "base_url", costrictAPI)
	}

	r := NewRouterWithOptions(pool, hub, bus, analyticsClient, storeRedis, RouterOptions{
		HTTPMetrics:        httpMetrics,
		DaemonHub:          daemonHub,
		DaemonWakeup:       daemonWakeup,
		HeartbeatScheduler: heartbeatScheduler,
		JWKSProvider:       jwksProvider,
		SubjectResolver:    subjectResolver,
		CasdoorEnabled:     casdoorEnabled,
		SkillProxy:         skillProxy,
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	taskSvc := service.NewTaskService(queries, pool, hub, bus, daemonWakeup)
	taskSvc.Analytics = analyticsClient
	autopilotSvc := service.NewAutopilotService(queries, pool, bus, taskSvc)
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
	go runRuntimeSweeper(sweepCtx, queries, liveness, taskSvc, bus)
	go heartbeatScheduler.Run(sweepCtx)
	go runAutopilotScheduler(autopilotCtx, queries, autopilotSvc)
	go runAutopilotFailureMonitor(autopilotCtx, queries, bus, envFailureMonitorConfig())
	go runDBStatsLogger(sweepCtx, pool)

	if metricsServer != nil {
		go func() {
			slog.Info("metrics server starting", "addr", metricsConfig.Addr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server disabled after startup error", "error", err)
			}
		}()
	}

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	autopilotCancel()

	// Order matters: drain in-flight HTTP first so any heartbeat handlers
	// finish calling Schedule() before we stop the scheduler. Otherwise a
	// late heartbeat could enqueue a pending ID after Run has already
	// drained and exited, and Stop() would not flush it.
	apiShutdownCtx, apiShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := srv.Shutdown(apiShutdownCtx); err != nil {
		apiShutdownCancel()
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	apiShutdownCancel()

	// HTTP is fully drained — safe to stop the sweeper and flush the
	// final batch of queued heartbeat bumps.
	sweepCancel()
	heartbeatScheduler.Stop()

	if metricsServer != nil {
		metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
			slog.Error("metrics server forced to shutdown", "error", err)
		}
		metricsShutdownCancel()
	}
	slog.Info("server stopped")
}
