package rounds

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIR-3114 — "batch means: answer everything, then it all runs at once", now
// through the answer-cycle lifecycle (review round 3): Start dispatches held
// replies and opens a cycle; the dispatch run completes silently when the
// agent responds (the response queues instead of re-surfacing the round); the
// next Start surfaces it as waiting; and answering the last waiting message
// releases the held replies and closes the cycle — round done, no Run press.
func TestHoldCommentAutoStartsRoundWhenEveryResponseIsAnswered(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	queries := db.New(pool)

	var wsID, ownerID, runtimeID, agentID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Rounds Auto', 'rounds-auto-'||substr(gen_random_uuid()::text,1,8), '', 'RAU') RETURNING id`).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM workspace WHERE id=$1`, wsID)
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Auto Owner', 'rounds-auto-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, ownerID)
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status) VALUES ($1, 'Auto Runtime', 'local', 'claude_code', 'online') RETURNING id`, wsID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id) VALUES ($1, 'Auto Agent', 'local', $2) RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO issue (workspace_id, title, number, creator_type, creator_id, assignee_type, assignee_id) VALUES ($1, 'Auto issue', floor(random()*1000000)::int, 'member', $2, 'agent', $3) RETURNING id`, wsID, ownerID, agentID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	newComment := func(authorType string, authorID pgtype.UUID, content string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content) VALUES ($1, $2, $3, $4, $5) RETURNING id`, wsID, issueID, authorType, authorID, content).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cycleOpen := func() bool {
		t.Helper()
		var open bool
		if err := pool.QueryRow(ctx, `SELECT cycle_opened_at IS NOT NULL FROM cerebro_round WHERE workspace_id=$1`, wsID).Scan(&open); err != nil {
			t.Fatal(err)
		}
		return open
	}

	svc := New(pool, queries, service.NewTaskService(queries, pool, nil, events.New()))
	round, err := svc.Create(ctx, wsID, ownerID, "Auto", "batch", "", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	roundID := mustUUID(t, round.ID)
	if _, err := svc.AddMember(ctx, wsID, ownerID, roundID, issueID, "member", ownerID); err != nil {
		t.Fatal(err)
	}

	held, err := svc.HoldComment(ctx, wsID, issueID, newComment("member", ownerID, "Question for the agent"), "member", util.UUIDToString(ownerID), "Question for the agent")
	if err != nil || !held {
		t.Fatalf("first HoldComment = %v held=%v, want held", err, held)
	}
	run, err := svc.Start(ctx, wsID, ownerID, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunRunning || run.TotalCount != 1 {
		t.Fatalf("run after start = %q total=%d, want running/1", run.Status, run.TotalCount)
	}
	if cycleOpen() {
		t.Fatal("cycle open right after dispatch with nothing to answer, want closed (round is working, not waiting)")
	}

	// The agent's task completes and it posts its response: the run completes
	// silently and the response queues — it must NOT re-surface the round
	// (review round 3, point 3).
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=(SELECT task_id FROM cerebro_round_run_item WHERE run_id=$1)`, mustUUID(t, run.ID)); err != nil {
		t.Fatal(err)
	}
	newComment("agent", agentID, "Agent response")
	svc.RefreshRun(ctx, mustUUID(t, run.ID))
	if refreshed, err := svc.GetRun(ctx, mustUUID(t, run.ID)); err != nil || refreshed.Status != RunCompleted {
		t.Fatalf("run after agent response = %+v (%v), want completed (silent)", refreshed, err)
	}
	members, err := svc.Members(ctx, roundID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members = %+v (%v), want 1", members, err)
	}
	if members[0].State != MemberQueued || members[0].WaitingCount != 0 {
		t.Fatalf("member before next start = state %q waiting %d, want queued/0", members[0].State, members[0].WaitingCount)
	}

	// The owner starts the next round: the queued response surfaces.
	if _, err := svc.Start(ctx, wsID, ownerID, roundID); err != nil {
		t.Fatal(err)
	}
	if !cycleOpen() {
		t.Fatal("cycle closed after start with a queued response, want open")
	}
	members, err = svc.Members(ctx, roundID)
	if err != nil || len(members) != 1 || members[0].State != MemberWaiting || members[0].WaitingCount != 1 {
		t.Fatalf("member after next start = %+v (%v), want waiting/1", members, err)
	}

	// The owner answers the only waiting response — the held reply releases
	// and the cycle closes on its own: round done, no Run press.
	held, err = svc.HoldComment(ctx, wsID, issueID, newComment("member", ownerID, "My answer"), "member", util.UUIDToString(ownerID), "My answer")
	if err != nil || !held {
		t.Fatalf("answer HoldComment = %v held=%v, want held", err, held)
	}
	active, err := svc.ActiveRun(ctx, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.ID == run.ID || active.TotalCount != 1 {
		t.Fatalf("active run after answering everything = %+v, want a new run releasing the 1 held answer", active)
	}
	if cycleOpen() {
		t.Fatal("cycle still open after answering everything, want closed (round done)")
	}
	var heldLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held'`, roundID).Scan(&heldLeft); err != nil {
		t.Fatal(err)
	}
	if heldLeft != 0 {
		t.Fatalf("held triggers after auto-release = %d, want 0 (released)", heldLeft)
	}
}
