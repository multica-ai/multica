// audit_issue_lifecycle verifies the additive lifecycle projection after a
// deploy or backfill. A non-zero exit means at least one workspace or issue is
// inconsistent with the legacy compatibility columns.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type report struct {
	WorkspaceID                  string `json:"workspace_id"`
	WorkspacesWithoutDefault     int64  `json:"workspaces_without_default"`
	IssuesWithoutBinding         int64  `json:"issues_without_binding"`
	IssuesWithStatusMismatch     int64  `json:"issues_with_status_mismatch"`
	IssuesWithTransitionMismatch int64  `json:"issues_with_transition_mismatch"`
}

func (r report) consistent() bool {
	return r.WorkspacesWithoutDefault == 0 &&
		r.IssuesWithoutBinding == 0 &&
		r.IssuesWithStatusMismatch == 0 &&
		r.IssuesWithTransitionMismatch == 0
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	rows, err := db.New(pool).ListIssueLifecycleConsistency(ctx)
	if err != nil {
		return fmt.Errorf("audit issue lifecycle: %w", err)
	}
	reports := make([]report, 0, len(rows))
	consistent := true
	for _, row := range rows {
		r := report{
			WorkspaceID:                  util.UUIDToString(row.WorkspaceID),
			WorkspacesWithoutDefault:     row.WorkspacesWithoutDefault,
			IssuesWithoutBinding:         row.IssuesWithoutBinding,
			IssuesWithStatusMismatch:     row.IssuesWithStatusMismatch,
			IssuesWithTransitionMismatch: row.IssuesWithTransitionMismatch,
		}
		reports = append(reports, r)
		consistent = consistent && r.consistent()
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"consistent": consistent,
		"workspaces": reports,
	}); err != nil {
		return fmt.Errorf("encode audit report: %w", err)
	}
	if !consistent {
		return errors.New("issue lifecycle consistency check failed")
	}
	return nil
}
