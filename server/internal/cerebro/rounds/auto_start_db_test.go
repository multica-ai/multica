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

// FIR-3114 — "batch means: answer everything, then it all runs at once". Once
// the owner has replied to every agent response in the surfaced 'ready' run,
// HoldComment auto-starts the round: the ready run completes and a new run
// releases the held replies without pressing Run.
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
	newComment := func(content string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content) VALUES ($1, $2, 'member', $3, $4) RETURNING id`, wsID, issueID, ownerID, content).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
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

	held, err := svc.HoldComment(ctx, wsID, issueID, newComment("Question for the agent"), "member", util.UUIDToString(ownerID), "Question for the agent")
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

	// The agent responds: its task completes and the run turns 'ready'.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=(SELECT task_id FROM cerebro_round_run_item WHERE run_id=$1)`, mustUUID(t, run.ID)); err != nil {
		t.Fatal(err)
	}
	svc.RefreshRun(ctx, mustUUID(t, run.ID))
	if refreshed, err := svc.GetRun(ctx, mustUUID(t, run.ID)); err != nil || refreshed.Status != RunReady {
		t.Fatalf("run after agent response = %+v (%v), want ready", refreshed, err)
	}

	// The owner answers the only response — the round auto-starts.
	held, err = svc.HoldComment(ctx, wsID, issueID, newComment("My answer"), "member", util.UUIDToString(ownerID), "My answer")
	if err != nil || !held {
		t.Fatalf("answer HoldComment = %v held=%v, want held", err, held)
	}
	previous, err := svc.GetRun(ctx, mustUUID(t, run.ID))
	if err != nil {
		t.Fatal(err)
	}
	if previous.Status != RunCompleted {
		t.Fatalf("previous run after auto-start = %q, want completed", previous.Status)
	}
	active, err := svc.ActiveRun(ctx, roundID)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.ID == run.ID || active.TotalCount != 1 {
		t.Fatalf("active run after auto-start = %+v, want a new run releasing the 1 held answer", active)
	}
	var heldLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held'`, roundID).Scan(&heldLeft); err != nil {
		t.Fatal(err)
	}
	if heldLeft != 0 {
		t.Fatalf("held triggers after auto-start = %d, want 0 (released)", heldLeft)
	}
}
