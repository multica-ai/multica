package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// JEH-1035 follow-up: resume-scope narrowing. The old query swept every
// transient-fail leaf in a 24h window into the resume set, so a 5-min
// pause could resurrect 20+ unrelated failures from earlier in the day.
// The new query keys on (paused_at - 10 min .. paused_at) for transient
// reasons, and always returns runtime_paused leaves (those are by
// construction the pause's own victims).
//
// Skipped when DATABASE_URL is unreachable — same skip mechanism as
// account_test.go (shared TestMain in this package).

func TestListResumableTasksForRuntime_ScopeNarrowing(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping resume-scope integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	runtimeID := runtimeAccountTestRuntimeID
	wsID := runtimeAccountTestWSID

	// Create an agent + issue scoped to the shared workspace; clean up at end.
	var agentID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'resume-scope-test-agent', 'local', $2)
		RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'resume-scope-test-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// Anchor: pause started at T=0. Tasks placed at -30m / -5m / -2m / +2m
	// relative to T=0.
	pausedAt := time.Now().UTC().Truncate(time.Second)

	insertTask := func(reason string, completedAt time.Time, parentID pgtype.UUID) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, priority,
				completed_at, failure_reason, parent_task_id, error
			)
			VALUES ($1, $2, $3, 'failed', 0, $4, $5, $6, 'test')
			RETURNING id`,
			agentID, issueID, runtimeID, completedAt, reason, parentID).Scan(&id)
		if err != nil {
			t.Fatalf("insert task (reason=%s): %v", reason, err)
		}
		return id
	}
	nullUUID := pgtype.UUID{}

	// In-flight when pause kicked in — always resumable.
	pausedTask := insertTask("runtime_paused", pausedAt.Add(-30*time.Second), nullUUID)
	// Triggered the pause: rate_limit 5 min before pausedAt → in window.
	triggerTask := insertTask("rate_limit", pausedAt.Add(-5*time.Minute), nullUUID)
	// Just outside the 10-min window — unrelated to this pause episode.
	staleTransient := insertTask("rate_limit", pausedAt.Add(-30*time.Minute), nullUUID)
	// Future relative to pausedAt — cannot have triggered the pause.
	postPauseTransient := insertTask("rate_limit", pausedAt.Add(2*time.Minute), nullUUID)
	// Non-transient failure within window — not pause-related (agent bug).
	agentErrorInWindow := insertTask("agent_error", pausedAt.Add(-3*time.Minute), nullUUID)
	// runtime_paused that has already been resumed (has a child) — leaf-filter skip.
	alreadyResumed := insertTask("runtime_paused", pausedAt.Add(-1*time.Minute), nullUUID)
	_ = insertTask("runtime_paused", pausedAt.Add(-30*time.Second), alreadyResumed)

	rows, err := queries.ListResumableTasksForRuntime(ctx, cerebrodb.ListResumableTasksForRuntimeParams{
		RuntimeID: runtimeID,
		PausedAt:  pgtype.Timestamptz{Time: pausedAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListResumableTasksForRuntime: %v", err)
	}

	got := map[pgtype.UUID]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}

	wantIn := map[string]pgtype.UUID{
		"pausedTask":  pausedTask,
		"triggerTask": triggerTask,
	}
	wantOut := map[string]pgtype.UUID{
		"staleTransient":     staleTransient,
		"postPauseTransient": postPauseTransient,
		"agentErrorInWindow": agentErrorInWindow,
		"alreadyResumed":     alreadyResumed,
	}
	for name, id := range wantIn {
		if !got[id] {
			t.Errorf("expected %s (%v) to be in resume set, was not", name, id)
		}
	}
	for name, id := range wantOut {
		if got[id] {
			t.Errorf("expected %s (%v) to be excluded from resume set, was included", name, id)
		}
	}
}

// When paused_at is NULL (e.g. unpause-of-unpaused or snapshot read failed)
// the transient branch is short-circuited and only runtime_paused leaves
// come back. Keeps idempotent unpause safe and recovers any orphaned
// runtime_paused leaves from a prior crashed unpause.
func TestListResumableTasksForRuntime_NullPausedAtFallsBackToRuntimePausedOnly(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping resume-scope integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	runtimeID := runtimeAccountTestRuntimeID
	wsID := runtimeAccountTestWSID

	var agentID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'resume-scope-null-agent', 'local', $2)
		RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'resume-scope-null-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	now := time.Now().UTC()
	nullUUID := pgtype.UUID{}
	insertTask := func(reason string, completedAt time.Time) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, priority,
				completed_at, failure_reason, parent_task_id, error
			)
			VALUES ($1, $2, $3, 'failed', 0, $4, $5, $6, 'test')
			RETURNING id`,
			agentID, issueID, runtimeID, completedAt, reason, nullUUID).Scan(&id)
		if err != nil {
			t.Fatalf("insert task: %v", err)
		}
		return id
	}

	pausedLeaf := insertTask("runtime_paused", now.Add(-1*time.Minute))
	transient := insertTask("rate_limit", now.Add(-2*time.Minute))

	rows, err := queries.ListResumableTasksForRuntime(ctx, cerebrodb.ListResumableTasksForRuntimeParams{
		RuntimeID: runtimeID,
		PausedAt:  pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		t.Fatalf("ListResumableTasksForRuntime: %v", err)
	}

	got := map[pgtype.UUID]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[pausedLeaf] {
		t.Errorf("expected runtime_paused leaf to be included when paused_at is NULL")
	}
	if got[transient] {
		t.Errorf("expected transient leaf to be excluded when paused_at is NULL")
	}
}
