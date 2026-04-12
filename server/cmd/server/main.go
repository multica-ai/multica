package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/sandbox"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

	// Parse encryption key (used by both router/handler and CloudDaemon)
	var encKey []byte
	if hexKey := os.Getenv("ENCRYPTION_KEY"); hexKey != "" {
		var keyErr error
		encKey, keyErr = crypto.ParseHexKey(hexKey)
		if keyErr != nil {
			slog.Error("invalid ENCRYPTION_KEY", "error", keyErr)
			os.Exit(1)
		}
	}

	// Shared PingStore — used by both HTTP handler and CloudDaemon.
	pingStore := handler.NewPingStore()

	r := NewRouter(pool, hub, bus, encKey, pingStore)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start CloudDaemon BEFORE sweeper so first heartbeat runs before first sweep tick.
	var cloudDaemon *sandbox.CloudDaemon

	taskService := service.NewTaskService(queries, hub, bus)
	cloudDaemon = sandbox.NewCloudDaemon(sandbox.CloudDaemonConfig{
		Queries:       queries,
		TaskService:   taskService,
		Bus:           bus,
		EncryptionKey: encKey,
		PingChecker:   &pingStoreAdapter{store: pingStore},
	})
	cloudDaemonCtx, cloudDaemonCancel := context.WithCancel(context.Background())
	if cloudDaemon != nil {
		cloudDaemon.Start(cloudDaemonCtx)
	}

	// Start background sweeper to mark stale runtimes as offline.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	go runRuntimeSweeper(sweepCtx, queries, bus)

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

	// Stop CloudDaemon first (drains active tasks)
	cloudDaemonCancel()
	if cloudDaemon != nil {
		cloudDaemon.Stop()
	}

	sweepCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

// pingStoreAdapter adapts handler.PingStore to the sandbox.PingChecker interface.
type pingStoreAdapter struct {
	store *handler.PingStore
}

func (a *pingStoreAdapter) PopPending(runtimeID string) (string, bool) {
	p := a.store.PopPending(runtimeID)
	if p == nil {
		return "", false
	}
	return p.ID, true
}

func (a *pingStoreAdapter) Complete(pingID, output string, durationMs int64) {
	a.store.Complete(pingID, output, durationMs)
}

func (a *pingStoreAdapter) Fail(pingID, errMsg string, durationMs int64) {
	a.store.Fail(pingID, errMsg, durationMs)
}
