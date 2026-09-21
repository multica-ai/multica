package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// I4127.DP / I4192.DP: IssueService.Create is the shared create entry for
// every transport (HTTP, Lark, channel, future MCP/backfill/admin), so the
// in_progress-requires-assignee invariant must live here, not only in the
// HTTP handler. These tests pin the service-layer guard; the DB CHECK from
// migration 349 backs it up for raw-SQL paths (covered by the migration
// test in server/internal/migrations).
func TestIssueServiceCreate_InProgressRequiresAssignee(t *testing.T) {
	ctx := context.Background()
	pool := newIssueServiceGuardPool(t)
	svc := NewIssueService(db.New(pool), pool, events.New(), analytics.NoopClient{}, nil)

	userID, workspaceID := createIssueServiceGuardFixture(t, ctx, pool)

	memberAssignee := func() (pgtype.Text, pgtype.UUID) {
		return pgtype.Text{String: "member", Valid: true},
			pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true}
	}

	t.Run("in_progress without assignee is rejected", func(t *testing.T) {
		_, err := svc.Create(ctx, IssueCreateParams{
			WorkspaceID: pgtype.UUID{Bytes: parseTestUUID(t, workspaceID), Valid: true},
			Title:       "zombie attempt",
			Status:      "in_progress",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
		}, IssueCreateOpts{})
		if !errors.Is(err, ErrInProgressRequiresAssignee) {
			t.Fatalf("in_progress without assignee: got %v, want ErrInProgressRequiresAssignee", err)
		}
	})

	t.Run("in_progress with assignee succeeds", func(t *testing.T) {
		at, aid := memberAssignee()
		res, err := svc.Create(ctx, IssueCreateParams{
			WorkspaceID:  pgtype.UUID{Bytes: parseTestUUID(t, workspaceID), Valid: true},
			Title:        "in_progress assigned",
			Status:       "in_progress",
			Priority:     "none",
			AssigneeType: at,
			AssigneeID:   aid,
			CreatorType:  "member",
			CreatorID:    pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
		}, IssueCreateOpts{})
		if err != nil {
			t.Fatalf("in_progress with assignee: %v", err)
		}
		if !res.Issue.ID.Valid {
			t.Fatal("in_progress with assignee: expected a created issue")
		}
	})

	t.Run("todo without assignee succeeds", func(t *testing.T) {
		res, err := svc.Create(ctx, IssueCreateParams{
			WorkspaceID: pgtype.UUID{Bytes: parseTestUUID(t, workspaceID), Valid: true},
			Title:       "unassigned todo",
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
		}, IssueCreateOpts{})
		if err != nil {
			t.Fatalf("todo without assignee: %v", err)
		}
		if !res.Issue.ID.Valid {
			t.Fatal("todo without assignee: expected a created issue")
		}
	})
}

// TestIssueServiceCreate_CustomStatusInProgressCategoryRequiresAssignee
// verifies the guard runs on the EFFECTIVE status (MUL-6243): a custom
// status whose category is in_progress is held to the same rule as the
// built-in, while a custom status in another category stays open to
// unassigned issues.
func TestIssueServiceCreate_CustomStatusInProgressCategoryRequiresAssignee(t *testing.T) {
	ctx := context.Background()
	pool := newIssueServiceGuardPool(t)
	svc := NewIssueService(db.New(pool), pool, events.New(), analytics.NoopClient{}, nil)

	userID, workspaceID := createIssueServiceGuardFixture(t, ctx, pool)
	wsUUID := pgtype.UUID{Bytes: parseTestUUID(t, workspaceID), Valid: true}

	suffix := time.Now().UnixNano() % 1_000_000
	progressKey := fmt.Sprintf("svc_progress_%d", suffix)
	todoKey := fmt.Sprintf("svc_todo_%d", suffix)
	for _, st := range []struct{ key, category string }{
		{progressKey, "in_progress"},
		{todoKey, "todo"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
			VALUES ($1, $2, $3, '', $4, '#123456', 1)
		`, workspaceID, st.key, st.key, st.category); err != nil {
			t.Fatalf("create custom status %s: %v", st.key, err)
		}
		t.Cleanup(func() {
			pool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, workspaceID, st.key)
		})
	}

	// Custom in_progress-category status without assignee -> zombie, refused.
	_, err := svc.Create(ctx, IssueCreateParams{
		WorkspaceID: wsUUID,
		Title:       "custom progress zombie",
		Status:      progressKey,
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
	}, IssueCreateOpts{})
	if !errors.Is(err, ErrInProgressRequiresAssignee) {
		t.Fatalf("custom in_progress-category without assignee: got %v, want ErrInProgressRequiresAssignee", err)
	}

	// Custom in_progress-category status WITH assignee -> fine.
	at := pgtype.Text{String: "member", Valid: true}
	aid := pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true}
	res, err := svc.Create(ctx, IssueCreateParams{
		WorkspaceID:  wsUUID,
		Title:        "custom progress assigned",
		Status:       progressKey,
		Priority:     "none",
		AssigneeType: at,
		AssigneeID:   aid,
		CreatorType:  "member",
		CreatorID:    pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("custom in_progress-category with assignee: %v", err)
	}
	if !res.Issue.ID.Valid {
		t.Fatal("custom in_progress-category with assignee: expected a created issue")
	}

	// Custom todo-category status without assignee -> not our concern, allowed.
	res, err = svc.Create(ctx, IssueCreateParams{
		WorkspaceID: wsUUID,
		Title:       "custom todo unassigned",
		Status:      todoKey,
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   pgtype.UUID{Bytes: parseTestUUID(t, userID), Valid: true},
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("custom todo-category without assignee: %v", err)
	}
	if !res.Issue.ID.Valid {
		t.Fatal("custom todo-category without assignee: expected a created issue")
	}
}

func newIssueServiceGuardPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createIssueServiceGuardFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (userID, workspaceID string) {
	t.Helper()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("issue-guard-%d@multica.ai", suffix)
	slug := fmt.Sprintf("issue-guard-%d", suffix)

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Issue Guard User", email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Issue Guard Tests", slug, "Temporary workspace for issue guard tests", "IGD").Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID, workspaceID
}

func parseTestUUID(t *testing.T, s string) [16]byte {
	t.Helper()
	return util.MustParseUUID(s).Bytes
}
