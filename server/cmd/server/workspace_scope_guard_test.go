package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestWorkspaceScopeGuard locks in the SQL-layer tenant guard added in PR #3027.
// For each scoped query, it creates a resource in workspace A (the integration
// fixture workspace), then invokes the query with a foreign workspace UUID and
// asserts the row is untouched:
//   - :exec queries return (0 rows affected, nil) — silent no-op.
//   - :one queries return pgx.ErrNoRows.
//
// If a future refactor drops the workspace_id arg from any of these queries,
// the cross-workspace call would mutate the row and this test will fail.
func TestWorkspaceScopeGuard(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	queries := db.New(testPool)
	wsA := parseUUID(testWorkspaceID)
	wsB := randomUUID(t) // never-existed workspace; the guard predicate must reject it

	t.Run("DeleteIssue", func(t *testing.T) {
		id := seedIssue(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: id, WorkspaceID: wsB}); err != nil {
			t.Fatalf("cross-workspace DeleteIssue: expected nil error (no-op), got %v", err)
		}
		assertRowExists(t, ctx, "issue", id)
	})

	t.Run("DeleteComment", func(t *testing.T) {
		issueID := seedIssue(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, util.UUIDToString(issueID)) })
		id := seedComment(t, ctx, issueID)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteComment(ctx, db.DeleteCommentParams{ID: id, WorkspaceID: wsB}); err != nil {
			t.Fatalf("cross-workspace DeleteComment: expected nil error (no-op), got %v", err)
		}
		assertRowExists(t, ctx, "comment", id)
	})

	t.Run("DeleteProject", func(t *testing.T) {
		id := seedProject(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteProject(ctx, db.DeleteProjectParams{ID: id, WorkspaceID: wsB}); err != nil {
			t.Fatalf("cross-workspace DeleteProject: expected nil error (no-op), got %v", err)
		}
		assertRowExists(t, ctx, "project", id)
	})

	t.Run("DeleteSkill", func(t *testing.T) {
		id := seedSkill(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteSkill(ctx, db.DeleteSkillParams{ID: id, WorkspaceID: wsB}); err != nil {
			t.Fatalf("cross-workspace DeleteSkill: expected nil error (no-op), got %v", err)
		}
		assertRowExists(t, ctx, "skill", id)
	})

	t.Run("DeleteChatSession", func(t *testing.T) {
		id := seedChatSession(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteChatSession(ctx, db.DeleteChatSessionParams{ID: id, WorkspaceID: wsB}); err != nil {
			t.Fatalf("cross-workspace DeleteChatSession: expected nil error (no-op), got %v", err)
		}
		assertRowExists(t, ctx, "chat_session", id)
	})

	t.Run("UpdateIssueStatus", func(t *testing.T) {
		id := seedIssue(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, util.UUIDToString(id)) })

		_, err := queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          id,
			Status:      "in_progress",
			WorkspaceID: wsB,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-workspace UpdateIssueStatus: expected pgx.ErrNoRows, got %v", err)
		}

		// Status must still be the original 'todo'.
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, util.UUIDToString(id)).Scan(&status); err != nil {
			t.Fatalf("re-read issue: %v", err)
		}
		if status != "todo" {
			t.Fatalf("issue status changed across workspace boundary: got %q, want 'todo'", status)
		}
	})

	// Sanity check: a buggy guard that returns no-op for every call would
	// also satisfy the cross-workspace assertions above. This sub-test
	// proves the in-workspace path still mutates.
	t.Run("InWorkspaceCallsStillWork", func(t *testing.T) {
		id := seedIssue(t, ctx)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, util.UUIDToString(id)) })

		if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: id, WorkspaceID: wsA}); err != nil {
			t.Fatalf("in-workspace DeleteIssue: %v", err)
		}
		var count int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id = $1`, util.UUIDToString(id)).Scan(&count); err != nil {
			t.Fatalf("count issue: %v", err)
		}
		if count != 0 {
			t.Fatalf("in-workspace DeleteIssue did not remove the row")
		}
	})
}

// ---- seed helpers (resource lives in testWorkspaceID) ----

func seedIssue(t *testing.T, ctx context.Context) pgtype.UUID {
	t.Helper()
	var s string
	// number is unique per workspace; pick a high-range random value to
	// avoid colliding with concurrent integration tests in the same DB.
	n := 1_000_000 + rand.IntN(1_000_000)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, 'scope-guard test issue', 'todo', 'none', 'member', $2, 0, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, n).Scan(&s); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return parseUUID(s)
}

func seedComment(t *testing.T, ctx context.Context, issueID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var s string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'scope-guard test comment', 'comment')
		RETURNING id
	`, util.UUIDToString(issueID), testWorkspaceID, testUserID).Scan(&s); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	return parseUUID(s)
}

func seedProject(t *testing.T, ctx context.Context) pgtype.UUID {
	t.Helper()
	var s string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, 'scope-guard test project', 'planned', 'none')
		RETURNING id
	`, testWorkspaceID).Scan(&s); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return parseUUID(s)
}

func seedSkill(t *testing.T, ctx context.Context) pgtype.UUID {
	t.Helper()
	var s string
	// skill name is UNIQUE per workspace; add a random suffix to avoid colliding
	// with previous runs on the same DB.
	name := uniqueName("scope-guard skill")
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, '', '', '{}'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&s); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	return parseUUID(s)
}

func seedChatSession(t *testing.T, ctx context.Context) pgtype.UUID {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}
	var s string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES ($1, $2, $3, 'scope-guard chat', (SELECT runtime_id FROM agent WHERE id = $2))
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&s); err != nil {
		t.Fatalf("seed chat_session: %v", err)
	}
	return parseUUID(s)
}

func assertRowExists(t *testing.T, ctx context.Context, table string, id pgtype.UUID) {
	t.Helper()
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE id = $1`, util.UUIDToString(id)).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("row in %s was removed across workspace boundary (count = %d)", table, count)
	}
}

// randomUUID returns a never-existed workspace UUID for cross-tenant probes.
func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom: %v", err)
	}
	return parseUUID(u.String())
}

// uniqueName returns prefix + a short random suffix to avoid UNIQUE collisions
// across reruns on the same database.
func uniqueName(prefix string) string {
	u, err := uuid.NewRandom()
	if err != nil {
		return prefix
	}
	return prefix + "-" + u.String()[:8]
}
