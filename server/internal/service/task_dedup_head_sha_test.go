package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TEN-356 regression: the reviewer-loop dedup keyed only on (issue_id,
// agent_id), so a completed/pending verdict for commit A was silently reused to
// satisfy a review request for a NEWER commit B pushed after A's run began —
// giving B zero review coverage. The fix stamps the reviewed head SHA into the
// task's context JSONB and keys HasPendingTaskForIssueAndAgent on it. These
// tests pin the three behaviors at the query layer, where the dedup decision
// actually lives.

func newHeadShaDedupPool(t *testing.T) *pgxpool.Pool {
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

// headShaDedupFixture builds a workspace / runtime / agent / issue, plus an
// optional linked PR, and returns the identifiers the dedup tests need. When
// prHeadSha is non-empty a github_pull_request row is created and linked to the
// issue so ResolveIssueReviewSHA has a head to find.
type headShaDedupFixture struct {
	agentID   pgtype.UUID
	runtimeID pgtype.UUID
	issueID   pgtype.UUID
}

func createHeadShaDedupFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, prHeadSha, prState string) headShaDedupFixture {
	t.Helper()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("head-sha-dedup-%d@multica.ai", suffix)
	slug := fmt.Sprintf("head-sha-dedup-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Head SHA Dedup Test", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "Head SHA Dedup Test", slug, "temporary TEN-356 dedup test workspace", "HSD").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'head_sha_dedup_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "Head SHA Dedup Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Head SHA Dedup Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, 'in_review', 'none', $3, 'member', $4, 0)
		RETURNING id
	`, workspaceID, "head sha dedup issue", userID, 970000+int(suffix%1000)).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	if prHeadSha != "" {
		state := prState
		if state == "" {
			state = "open"
		}
		var prID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO github_pull_request (
				workspace_id, installation_id, repo_owner, repo_name, pr_number,
				title, state, html_url, pr_created_at, pr_updated_at, head_sha
			)
			VALUES ($1, 1, 'multica-ai', 'multica', $2, 'review PR', $3,
				'https://example.test/pr', now(), now(), $4)
			RETURNING id
		`, workspaceID, 4000+int(suffix%1000), state, prHeadSha).Scan(&prID); err != nil {
			t.Fatalf("create pull request: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)
		`, issueID, prID); err != nil {
			t.Fatalf("link pull request: %v", err)
		}
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(c, `DELETE FROM issue_pull_request WHERE issue_id = $1`, issueID)
		pool.Exec(c, `DELETE FROM github_pull_request WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM issue WHERE id = $1`, issueID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return headShaDedupFixture{
		agentID:   util.MustParseUUID(agentID),
		runtimeID: util.MustParseUUID(runtimeID),
		issueID:   util.MustParseUUID(issueID),
	}
}

// enqueueReviewTask inserts a queued task carrying headSha in its context JSONB,
// mirroring what CreateAgentTask does at enqueue time.
func enqueueReviewTask(t *testing.T, ctx context.Context, q *db.Queries, fx headShaDedupFixture, headSha string) {
	t.Helper()
	if _, err := q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:   fx.agentID,
		RuntimeID: fx.runtimeID,
		IssueID:   fx.issueID,
		Priority:  0,
		HeadSha:   pgtype.Text{String: headSha, Valid: headSha != ""},
	}); err != nil {
		t.Fatalf("create review task (head_sha=%q): %v", headSha, err)
	}
}

func hasPending(t *testing.T, ctx context.Context, q *db.Queries, fx headShaDedupFixture, headSha string) bool {
	t.Helper()
	got, err := q.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: fx.issueID,
		AgentID: fx.agentID,
		HeadSha: pgtype.Text{String: headSha, Valid: headSha != ""},
	})
	if err != nil {
		t.Fatalf("HasPendingTaskForIssueAndAgent(head_sha=%q): %v", headSha, err)
	}
	return got
}

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// Behavior 1: a pending task for SHA A does NOT satisfy a request when HEAD has
// advanced to SHA B — dedup MISSES so a fresh review can enqueue against B.
func TestHeadShaDedup_AdvancedHeadMissesDedup(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	q := db.New(pool)
	fx := createHeadShaDedupFixture(t, ctx, pool, shaB, "open")

	// A run began against A and is still pending.
	enqueueReviewTask(t, ctx, q, fx, shaA)

	// A request for the new HEAD (B) must NOT dedup — the pending task is for A.
	if hasPending(t, ctx, q, fx, shaB) {
		t.Fatalf("dedup HIT for SHA B while only a SHA A task is pending — B would get zero review coverage (the TEN-356 bug)")
	}
}

// Behavior 2: re-pushing to a branch mid-review invalidates dedup. The resolver
// now returns the PR's new head, and a request for that head misses the old
// task — the platform-level equivalent of "fresh run against new HEAD".
func TestHeadShaDedup_RepushInvalidatesDedup(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	q := db.New(pool)
	// PR head starts at A.
	fx := createHeadShaDedupFixture(t, ctx, pool, shaA, "open")

	svc := NewTaskService(q, pool, nil, events.New())
	if got := svc.ResolveIssueReviewSHA(ctx, fx.issueID); got != shaA {
		t.Fatalf("ResolveIssueReviewSHA before repush = %q, want %q", got, shaA)
	}
	enqueueReviewTask(t, ctx, q, fx, shaA)
	if !hasPending(t, ctx, q, fx, shaA) {
		t.Fatalf("same-SHA request must dedup against the pending SHA A task")
	}

	// Simulate a mid-review force-push: PR head advances to B.
	if _, err := pool.Exec(ctx, `
		UPDATE github_pull_request SET head_sha = $1, pr_updated_at = now()
		WHERE workspace_id = (SELECT workspace_id FROM issue WHERE id = $2)
	`, shaB, util.UUIDToString(fx.issueID)); err != nil {
		t.Fatalf("advance PR head: %v", err)
	}

	// The resolver now reports B, and a dedup check for B misses the A task.
	if got := svc.ResolveIssueReviewSHA(ctx, fx.issueID); got != shaB {
		t.Fatalf("ResolveIssueReviewSHA after repush = %q, want %q", got, shaB)
	}
	if hasPending(t, ctx, q, fx, shaB) {
		t.Fatalf("dedup HIT for new HEAD B after repush — a fresh review would be suppressed")
	}
}

// Behavior 3: same-SHA re-requests still dedup, so an unchanged HEAD does not
// spawn wasteful duplicate reviewer runs.
func TestHeadShaDedup_SameShaStillDedups(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	q := db.New(pool)
	fx := createHeadShaDedupFixture(t, ctx, pool, shaA, "open")

	enqueueReviewTask(t, ctx, q, fx, shaA)

	if !hasPending(t, ctx, q, fx, shaA) {
		t.Fatalf("dedup MISS for the same SHA A — an unchanged HEAD should coalesce, not re-run")
	}
}

// Behavior 4 (fall-back safety): an issue with no linked PR has no review SHA,
// so dedup falls back to the pre-TEN-356 (issue_id, agent_id) key and keeps
// coalescing exactly as before.
func TestHeadShaDedup_NoLinkedPRFallsBackToLegacyKey(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	q := db.New(pool)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	svc := NewTaskService(q, pool, nil, events.New())
	if got := svc.ResolveIssueReviewSHA(ctx, fx.issueID); got != "" {
		t.Fatalf("ResolveIssueReviewSHA with no linked PR = %q, want empty", got)
	}

	// Enqueue with no head_sha (context NULL), then a legacy (empty-SHA) check
	// must still dedup.
	enqueueReviewTask(t, ctx, q, fx, "")
	if !hasPending(t, ctx, q, fx, "") {
		t.Fatalf("no-PR issue must dedup on (issue_id, agent_id) like pre-TEN-356")
	}
}
