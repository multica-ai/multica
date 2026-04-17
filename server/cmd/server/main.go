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
	r := NewRouter(pool, hub, bus, secretsCipher, gitlabClient, gitlabEnabled, serverCtx)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	taskSvc := service.NewTaskService(queries, hub, bus)
	autopilotSvc := service.NewAutopilotService(queries, pool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	// Start background sweeper to mark stale runtimes as offline.
	go runRuntimeSweeper(sweepCtx, queries, bus)
	go runAutopilotScheduler(autopilotCtx, queries, autopilotSvc)

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
