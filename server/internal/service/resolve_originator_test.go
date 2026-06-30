package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newResolveOriginatorPool mirrors the local-postgres pattern used in
// task_claim_race_test.go: skip when the test database is unreachable
// instead of failing, so `go test ./...` stays usable in CI / clean
// developer setups that don't run Postgres.
func newResolveOriginatorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
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

// seedOriginatorFanout builds the minimal fixture for an agent→agent
// fanout chain:
//
//	human U → (member-authored comment on issue I) →
//	agent A handles task T_A with originator_user_id = U →
//	agent A posts a reply comment C (author_type=agent, source_task_id=T_A) →
//	agent B picks up C as its trigger
//
// Returns: workspace id, member-authored comment id (C0), agent-authored
// comment id (C1, with source_task_id=T_A), and U as pgtype.UUID. T_A's
// originator_user_id is U so the agent-fanout branch can prove the
// inheritance.
func seedOriginatorFanout(t *testing.T, pool *pgxpool.Pool) (memberCommentID, agentCommentID, userID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	var workspaceID, agentAID, agentBID, runtimeID, issueID, taskAID, userIDStr, commentMemberID, commentAgentID string

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Resolve Originator User', 'resolve-originator-fanout@multica.test')
		RETURNING id
	`).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'resolve-originator-fanout@multica.test'`)
	})

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('resolve-orig-ws', 'resolve-orig-ws-' || gen_random_uuid())
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-A', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentAID); err != nil {
		t.Fatalf("seed agent A: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-B', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentBID); err != nil {
		t.Fatalf("seed agent B: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'fanout-issue', 'member', $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// Agent A's task carries the originator (the human U). This is the
	// row the resolver must follow back through comment.source_task_id.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, originator_user_id
		)
		VALUES ($1, $2, $3, 'completed', 0, $4)
		RETURNING id
	`, agentAID, runtimeID, issueID, userIDStr).Scan(&taskAID); err != nil {
		t.Fatalf("seed task A: %v", err)
	}

	// Member-authored comment (no source_task_id).
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'human comment')
		RETURNING id
	`, issueID, workspaceID, userIDStr).Scan(&commentMemberID); err != nil {
		t.Fatalf("seed member comment: %v", err)
	}

	// Agent-authored comment whose source_task_id points at task A.
	// resolveOriginatorFromTriggerComment must inherit task A's originator.
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'agent comment', $4)
		RETURNING id
	`, issueID, workspaceID, agentAID, taskAID).Scan(&commentAgentID); err != nil {
		t.Fatalf("seed agent comment: %v", err)
	}

	memberCommentID = util.MustParseUUID(commentMemberID)
	agentCommentID = util.MustParseUUID(commentAgentID)
	userID = util.MustParseUUID(userIDStr)
	return
}

// TestResolveOriginatorFromTriggerComment_MemberAuthored — the base case:
// a comment authored by a workspace member IS the top-of-chain. The
// originator is the comment's own author_id.
func TestResolveOriginatorFromTriggerComment_MemberAuthored(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	memberCommentID, _, userID := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), memberCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator for member-authored comment, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent
// — the load-bearing fanout case. Agent A finished a task it ran on
// behalf of human U; A then posts a comment that triggers agent B. The
// trigger comment's author is A (not a human), but resolving the
// originator must walk comment.source_task_id → parent task →
// parent.originator_user_id, yielding U.
func TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, agentCommentID, userID := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), agentCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator inherited from parent task, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s (parent task's originator_user_id)",
			util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorFromTriggerComment_InvalidCommentID — defensive
// branch. An invalid pgtype.UUID must short-circuit before any DB query
// and return invalid.
func TestResolveOriginatorFromTriggerComment_InvalidCommentID(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	svc := &TaskService{Queries: db.New(pool)}
	got := svc.resolveOriginatorFromTriggerComment(context.Background(), pgtype.UUID{})
	if got.Valid {
		t.Errorf("invalid comment id must yield invalid originator, got %s", util.UUIDToString(got))
	}
}
