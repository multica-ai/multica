package main

import (
	"context"
	cryptorand "crypto/rand"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

func main() {
	logger.Init()

	// Warn about missing configuration
	if os.Getenv("JWT_SECRET") == "" {
		slog.Warn("JWT_SECRET is not set — using insecure default. Set JWT_SECRET for production use.")
	}
	if os.Getenv("RESEND_API_KEY") == "" {
		slog.Warn("RESEND_API_KEY is not set — email verification codes will be printed to the log instead of emailed.")
	}

	// strconv.ParseBool accepts 1/t/T/TRUE/true/True (and the false equivalents),
	// so operators don't get caught out by writing TRUE/yes/1 in their env file.
	gitlabEnabled, _ := strconv.ParseBool(os.Getenv("MULTICA_GITLAB_ENABLED"))
	gitlabClient := gitlab.NewClient(gitlab.DefaultBaseURL, &http.Client{Timeout: 30 * time.Second})

	publicURL := os.Getenv("MULTICA_PUBLIC_URL")
	if publicURL == "" && gitlabEnabled {
		slog.Warn("MULTICA_PUBLIC_URL is not set; gitlab webhook registration will be skipped (cache will go stale after sync)")
	}

	secretsCipher, sErr := secrets.Load()
	if sErr != nil {
		// When GitLab is enabled, the cipher will be used to encrypt PATs that
		// must survive restarts. Falling back to an ephemeral key would mean
		// stored PATs become unrecoverable on the next boot — fail fast instead.
		if gitlabEnabled {
			slog.Error("MULTICA_SECRETS_KEY is required when MULTICA_GITLAB_ENABLED=true (otherwise stored credentials become unrecoverable on restart)", "error", sErr)
			os.Exit(1)
		}
		slog.Warn("MULTICA_SECRETS_KEY not configured; generating an ephemeral dev key. Set MULTICA_SECRETS_KEY for production.", "error", sErr)
		ephemeral := make([]byte, 32)
		if _, err := cryptorand.Read(ephemeral); err != nil {
			slog.Error("failed to generate ephemeral secrets key", "error", err)
			os.Exit(1)
		}
		secretsCipher, _ = secrets.NewCipher(ephemeral)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
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

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	registerListeners(bus, hub)

	queries := db.New(pool)
	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	// NewRouterWithHandler constructs the handler and router — we need both
	// because the autopilot service (registered below) must share the same
	// AutopilotService instance the HTTP trigger-autopilot endpoint uses, and
	// must also get an IssueCreator wired to the handler's CreateIssueInternal
	// so autopilot-generated issues flow through GitLab write-through on
	// connected workspaces (Phase 4).
	//
	// Previously main.go constructed a second AutopilotService for the
	// scheduler / listeners, which left the one on the handler without
	// listeners wired. The consolidation is intentional: one service, one
	// wired IssueCreator, one set of listeners.
	r, h := NewRouterWithHandler(pool, hub, bus, secretsCipher, gitlabClient, gitlabEnabled, serverCtx, publicURL)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	// taskSvc is constructed separately from the handler's internal TaskService
	// because the webhook worker needs an enqueuer reference at startup; the
	// handler's task service is behind h and would require an extra accessor.
	// Both write to the same DB table — functionally equivalent.
	taskSvc := service.NewTaskService(queries, hub, bus)
	autopilotSvc := h.AutopilotService
	autopilotSvc.SetIssueCreator(h)
	registerAutopilotListeners(bus, autopilotSvc)

	// Start background sweeper to mark stale runtimes as offline.
	go runRuntimeSweeper(sweepCtx, queries, bus)
	go runAutopilotScheduler(autopilotCtx, queries, autopilotSvc)

	if gitlabEnabled {
		glQueries := db.New(pool)
		// Shared decrypter for the reconciler + webhook worker (the latter
		// only uses it as a safety net — reverse-resolution reads unencrypted
		// identity-mapping tables and doesn't call decrypt).
		decrypter := gitlabsync.NewCipherDecrypter(secretsCipher)

		// Webhook worker pool — drains gitlab_webhook_event into the cache.
		// TaskEnqueuer lets the issue-hook spawn agent tasks when a human
		// assigns ~agent::<slug> from gitlab.com (Phase 4), closing the gap
		// between the REST write-through path and webhook-initiated updates.
		webhookWorker := gitlabsync.NewWebhookWorker(glQueries, pool, 5, 250*time.Millisecond).
			WithDecrypter(decrypter).
			WithTaskEnqueuer(taskSvc)
		go webhookWorker.Run(serverCtx)

		// Reconciler — 5-minute drift catcher.
		reconciler := gitlabsync.NewReconciler(glQueries, gitlabClient, decrypter)
		go reconciler.Run(serverCtx)
	}

	// Graceful shutdown
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
	cancelServer() // tell the gitlab sync goroutine to stop
	sweepCancel()
	autopilotCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
