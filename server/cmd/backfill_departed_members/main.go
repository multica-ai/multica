// backfill_departed_members applies the member-removal asset rules
// (service.RetireMemberAssets, MUL-10284) to users removed from a workspace
// before those rules existed: their agents are archived, their product agents
// are handed to the workspace's longest-standing owner, their tasks are
// cancelled, their autopilots are paused, and their wakeups are stopped.
//
// Each (workspace, user) pair commits in its own transaction, so the command
// can be stopped and rerun at any point; a pair with nothing left to retire is
// no longer listed. --dry-run lists the pairs without changing anything.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("departed member backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dryRun := flag.Bool("dry-run", false, "list the affected (workspace, user) pairs without changing anything")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	queries := db.New(pool)
	holders, err := queries.ListDepartedMemberAssetHolders(ctx)
	if err != nil {
		return fmt.Errorf("list departed members: %w", err)
	}
	slog.Info("departed member backfill started", "pairs", len(holders), "dry_run", *dryRun)

	for _, holder := range holders {
		attrs := []any{"workspace_id", util.UUIDToString(holder.WorkspaceID), "user_id", util.UUIDToString(holder.UserID)}
		if *dryRun {
			slog.Info("departed member holds live assets", attrs...)
			continue
		}
		retired, err := retire(ctx, pool, queries, holder)
		if err != nil {
			return fmt.Errorf("retire %s in %s: %w", util.UUIDToString(holder.UserID), util.UUIDToString(holder.WorkspaceID), err)
		}
		slog.Info("departed member assets retired", append(attrs,
			"agents_archived", len(retired.ArchivedAgents),
			"tasks_cancelled", len(retired.CancelledTasks),
			"product_agents_transferred", len(retired.TransferredAgents),
			"autopilots_paused", len(retired.PausedAutopilots),
			"issue_wakeups_disabled", retired.DisabledWakeups,
			"chat_wakeups_ended", retired.EndedChatWakeups,
		)...)
	}
	slog.Info("departed member backfill complete", "pairs", len(holders), "dry_run", *dryRun)
	return nil
}

func retire(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, holder db.ListDepartedMemberAssetHoldersRow) (service.RetiredMemberAssets, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return service.RetiredMemberAssets{}, err
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	// Re-check under the transaction: the user may have been re-invited since
	// the list was read.
	_, err = qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      holder.UserID,
		WorkspaceID: holder.WorkspaceID,
	})
	if err == nil {
		return service.RetiredMemberAssets{}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return service.RetiredMemberAssets{}, err
	}

	heir, err := service.MemberAssetHeir(ctx, qtx, holder.WorkspaceID, holder.UserID, pgtype.UUID{})
	if err != nil {
		return service.RetiredMemberAssets{}, err
	}
	retired, err := service.RetireMemberAssets(ctx, qtx, holder.WorkspaceID, holder.UserID, pgtype.UUID{}, heir)
	if err != nil {
		return service.RetiredMemberAssets{}, err
	}
	return retired, tx.Commit(ctx)
}
