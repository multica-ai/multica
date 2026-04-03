package main

// MULTICA-LOCAL: SQLite database, no PostgreSQL.

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func main() {
	logger.Init()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			slog.Error("unable to get home directory", "error", err)
			os.Exit(1)
		}
		dataDir := filepath.Join(homeDir, ".multica-local")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			slog.Error("unable to create data directory", "error", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(dataDir, "multica.db")
	}

	// Open SQLite database
	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		slog.Error("unable to open database", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// SQLite works best with a single writer connection
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	// Run migrations
	if err := runMigrations(sqlDB); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	slog.Info("connected to database", "path", dbPath)

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	registerListeners(bus, hub)

	queries := db.New(sqlDB)
	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	// MULTICA-LOCAL: Auto-provision local user and workspace on first launch.
	ensureLocalUser(context.Background(), sqlDB, queries)

	r := NewRouter(sqlDB, hub, bus)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	go runRuntimeSweeper(sweepCtx, queries, bus)

	// MULTICA-LOCAL: Start embedded daemon for agent task execution.
	taskSvc := service.NewTaskService(queries, hub, bus)
	daemon := NewEmbeddedDaemon(queries, taskSvc)
	daemon.RegisterRuntimes(context.Background())
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	go daemon.Run(daemonCtx)

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
	daemonCancel()
	sweepCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

// runMigrations applies SQLite migrations on startup.
func runMigrations(sqlDB *sql.DB) error {
	migrationSQL, err := os.ReadFile(findMigrationsFile())
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(string(migrationSQL))
	return err
}

func findMigrationsFile() string {
	candidates := []string{
		"migrations-sqlite/001_init.up.sql",
		"server/migrations-sqlite/001_init.up.sql",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

// ensureLocalUser creates the default local user and workspace if they don't exist.
func ensureLocalUser(ctx context.Context, sqlDB *sql.DB, queries *db.Queries) {
	const localEmail = "local@multica-local"
	_, err := queries.GetUserByEmail(ctx, localEmail)
	if err == nil {
		return // User already exists
	}

	slog.Info("first launch: creating local user and workspace")

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return
	}
	defer tx.Rollback()

	qtx := queries.WithTx(tx)

	user, err := qtx.CreateUser(ctx, db.CreateUserParams{
		ID:    newLocalUUID(),
		Name:  "Local User",
		Email: localEmail,
	})
	if err != nil {
		slog.Error("failed to create local user", "error", err)
		return
	}

	ws, err := qtx.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		ID:          newLocalUUID(),
		Name:        "My Workspace",
		Slug:        "local",
		IssuePrefix: "LOC",
	})
	if err != nil {
		slog.Error("failed to create workspace", "error", err)
		return
	}

	_, err = qtx.CreateMember(ctx, db.CreateMemberParams{
		ID:          newLocalUUID(),
		WorkspaceID: ws.ID,
		UserID:      user.ID,
		Role:        "owner",
	})
	if err != nil {
		slog.Error("failed to create member", "error", err)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit local user setup", "error", err)
		return
	}

	slog.Info("local user created", "user_id", user.ID, "workspace_id", ws.ID)
}

func newLocalUUID() string {
	return uuid.New().String()
}
