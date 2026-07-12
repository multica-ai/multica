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

type reviewFixture struct {
	pool      *pgxpool.Pool
	svc       *Service
	wsID      pgtype.UUID
	ownerID   pgtype.UUID
	agentID   pgtype.UUID
	runtimeID pgtype.UUID
	roundID   pgtype.UUID
}

// newReviewFixture builds a workspace, owner, agent and one batch round —
// the shared scaffolding for the FIR-3114 review tests below.
func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &reviewFixture{pool: pool}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Rounds Review', 'rounds-review-'||substr(gen_random_uuid()::text,1,8), '', 'RRV') RETURNING id`).Scan(&f.wsID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, f.wsID) })
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Review Owner', 'rounds-review-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&f.ownerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, f.ownerID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, f.wsID, f.ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status) VALUES ($1, 'Review Runtime', 'local', 'claude_code', 'online') RETURNING id`, f.wsID).Scan(&f.runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id) VALUES ($1, 'Review Agent', 'local', $2) RETURNING id`, f.wsID, f.runtimeID).Scan(&f.agentID); err != nil {
		t.Fatal(err)
	}
	f.svc = New(pool, db.New(pool), service.NewTaskService(db.New(pool), pool, nil, events.New()))
	round, err := f.svc.Create(ctx, f.wsID, f.ownerID, "Review", "batch", "", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	f.roundID = mustUUID(t, round.ID)
	return f
}

func (f *reviewFixture) newIssue(t *testing.T, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO issue (workspace_id, title, number, creator_type, creator_id, assignee_type, assignee_id) VALUES ($1, $2, floor(random()*1000000)::int, 'member', $3, 'agent', $4) RETURNING id`, f.wsID, title, f.ownerID, f.agentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddMember(context.Background(), f.wsID, f.ownerID, f.roundID, id, "member", f.ownerID); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *reviewFixture) newComment(t *testing.T, issueID pgtype.UUID, authorType string, authorID pgtype.UUID, content string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content) VALUES ($1, $2, $3, $4, $5) RETURNING id`, f.wsID, issueID, authorType, authorID, content).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// openCycle marks the round's answer cycle as open, as Start would, without
// dispatching anything — so tests can stage responses "surfaced by the cycle"
// (created before) versus "queued for the next start" (created after).
func (f *reviewFixture) openCycle(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE cerebro_round SET cycle_opened_at=now() WHERE id=$1`, f.roundID); err != nil {
		t.Fatal(err)
	}
}

func (f *reviewFixture) cycleOpen(t *testing.T) bool {
	t.Helper()
	var open bool
	if err := f.pool.QueryRow(context.Background(), `SELECT cycle_opened_at IS NOT NULL FROM cerebro_round WHERE id=$1`, f.roundID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	return open
}

func (f *reviewFixture) memberByIssue(t *testing.T, members []Member, issueID pgtype.UUID) Member {
	t.Helper()
	want := util.UUIDToString(issueID)
	for _, m := range members {
		if m.IssueID == want {
			return m
		}
	}
	t.Fatalf("member for issue %s not found in %+v", want, members)
	return Member{}
}

// FIR-3114 review, points 1+3 (round 3) — a member's state follows the
// owner's perspective, gated by the answer cycle: agent responses created
// before the cycle opened make it 'waiting'; responses arriving after the
// cycle opened are 'queued' for the next start and never surface on their
// own; the owner's reply makes it 'answered'; an in-flight agent task makes
// it 'working'; an untouched issue stays 'planned'.
func TestMemberStatesFollowOwnerPerspective(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	waiting := f.newIssue(t, "waiting: two agent responses")
	f.newComment(t, waiting, "agent", f.agentID, "first response")
	f.newComment(t, waiting, "agent", f.agentID, "second response")

	answered := f.newIssue(t, "answered: owner replied last")
	f.newComment(t, answered, "agent", f.agentID, "response")
	f.newComment(t, answered, "member", f.ownerID, "owner reply")

	working := f.newIssue(t, "working: agent task in flight")
	f.newComment(t, working, "agent", f.agentID, "response")
	f.newComment(t, working, "member", f.ownerID, "owner reply")
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status) VALUES ($1, $2, $3, 'running')`, f.agentID, working, f.runtimeID); err != nil {
		t.Fatal(err)
	}

	planned := f.newIssue(t, "planned: nothing happened yet")

	f.openCycle(t)

	queued := f.newIssue(t, "queued: response arrived after the cycle opened")
	f.newComment(t, queued, "agent", f.agentID, "late response")

	members, err := f.svc.Members(ctx, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if m := f.memberByIssue(t, members, waiting); m.State != MemberWaiting || m.WaitingCount != 2 {
		t.Fatalf("waiting member = state %q count %d, want waiting/2", m.State, m.WaitingCount)
	}
	if m := f.memberByIssue(t, members, answered); m.State != MemberAnswered || m.WaitingCount != 0 {
		t.Fatalf("answered member = state %q count %d, want answered/0", m.State, m.WaitingCount)
	}
	if m := f.memberByIssue(t, members, working); m.State != MemberWorking {
		t.Fatalf("working member = state %q, want working", m.State)
	}
	if m := f.memberByIssue(t, members, planned); m.State != MemberPlanned {
		t.Fatalf("planned member = state %q, want planned", m.State)
	}
	if m := f.memberByIssue(t, members, queued); m.State != MemberQueued || m.WaitingCount != 0 || m.QueuedCount != 1 {
		t.Fatalf("queued member = state %q waiting %d queued %d, want queued/0/1", m.State, m.WaitingCount, m.QueuedCount)
	}
}

// FIR-3114 review round 3, points 1+3 — with no open cycle NOTHING waits:
// every unanswered agent response is queued until the owner starts the round.
// This is the exact screenshot bug: the owner had answered everything, agents
// replied again within minutes, and the header showed "2 waiting".
func TestNothingWaitsWithoutAnOpenCycle(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	issue := f.newIssue(t, "answered, then the agent replied again")
	f.newComment(t, issue, "agent", f.agentID, "response")
	f.newComment(t, issue, "member", f.ownerID, "owner answer")
	f.newComment(t, issue, "agent", f.agentID, "follow-up after the answer")

	members, err := f.svc.Members(ctx, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if m := f.memberByIssue(t, members, issue); m.State != MemberQueued || m.WaitingCount != 0 || m.QueuedCount != 1 {
		t.Fatalf("member = state %q waiting %d queued %d, want queued/0/1 (nothing waits before the next start)", m.State, m.WaitingCount, m.QueuedCount)
	}
}

// FIR-3114 review, point 2 — a held reply must NOT start agents while other
// messages in the round still wait for the owner. Only when the owner has
// answered everything waiting anywhere in the round does the batch release.
func TestHoldCommentQueuesWhileOtherMessagesWait(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	issueA := f.newIssue(t, "issue A")
	f.newComment(t, issueA, "agent", f.agentID, "response A")
	issueB := f.newIssue(t, "issue B")
	f.newComment(t, issueB, "agent", f.agentID, "response B")

	// The open answer cycle whose surfaced responses the owner is answering.
	f.openCycle(t)

	replyA := f.newComment(t, issueA, "member", f.ownerID, "answer A")
	held, err := f.svc.HoldComment(ctx, f.wsID, issueA, replyA, "member", util.UUIDToString(f.ownerID), "answer A")
	if err != nil || !held {
		t.Fatalf("HoldComment A = %v held=%v, want held", err, held)
	}
	run, err := f.svc.ActiveRun(ctx, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if run != nil {
		t.Fatalf("active run after first answer = %+v, want none — issue B still waits", run)
	}
	if !f.cycleOpen(t) {
		t.Fatal("cycle closed after first answer, want still open — issue B still waits")
	}
	var heldCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held'`, f.roundID).Scan(&heldCount); err != nil {
		t.Fatal(err)
	}
	if heldCount != 1 {
		t.Fatalf("held triggers after first answer = %d, want 1 (still queued)", heldCount)
	}

	replyB := f.newComment(t, issueB, "member", f.ownerID, "answer B")
	held, err = f.svc.HoldComment(ctx, f.wsID, issueB, replyB, "member", util.UUIDToString(f.ownerID), "answer B")
	if err != nil || !held {
		t.Fatalf("HoldComment B = %v held=%v, want held", err, held)
	}
	run, err = f.svc.ActiveRun(ctx, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.TotalCount != 2 {
		t.Fatalf("active run after everything answered = %+v, want a run releasing both held replies", run)
	}
	if f.cycleOpen(t) {
		t.Fatal("cycle still open after everything answered, want closed (round done)")
	}
}

// FIR-3114 review, point 2 — a held reply while a run is 'running' queues for
// the next start instead of superseding the in-flight run.
func TestHoldCommentDoesNotStartWhileRunIsRunning(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	issueA := f.newIssue(t, "issue A")
	var runID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `INSERT INTO cerebro_round_run (round_id, total_count) VALUES ($1, 1) RETURNING id`, f.roundID).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	replyA := f.newComment(t, issueA, "member", f.ownerID, "answer A")
	held, err := f.svc.HoldComment(ctx, f.wsID, issueA, replyA, "member", util.UUIDToString(f.ownerID), "answer A")
	if err != nil || !held {
		t.Fatalf("HoldComment = %v held=%v, want held", err, held)
	}
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM cerebro_round_run WHERE id=$1`, runID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != RunRunning {
		t.Fatalf("running run after held reply = %q, want still running", status)
	}
	var heldCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_round_held_trigger WHERE round_id=$1 AND state='held'`, f.roundID).Scan(&heldCount); err != nil {
		t.Fatal(err)
	}
	if heldCount != 1 {
		t.Fatalf("held triggers = %d, want 1 (queued for the next start)", heldCount)
	}
}

// FIR-3114 review, point 4 — Pause works mid-run: DismissRun completes a
// 'running' run so the round collapses; already-dispatched tasks are left
// alone and unanswered messages simply stay waiting for the next round.
func TestDismissPausesRunningRun(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	f.openCycle(t)
	var runID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `INSERT INTO cerebro_round_run (round_id, total_count) VALUES ($1, 3) RETURNING id`, f.roundID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DismissRun(ctx, f.wsID, f.ownerID, f.roundID); err != nil {
		t.Fatal(err)
	}
	run, err := f.svc.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunCompleted || run.CompletedAt == nil {
		t.Fatalf("running run after pause = %q (completed_at %v), want completed with timestamp", run.Status, run.CompletedAt)
	}
	if active, err := f.svc.ActiveRun(ctx, f.roundID); err != nil || active != nil {
		t.Fatalf("active run after pause = %+v (%v), want none", active, err)
	}
	if f.cycleOpen(t) {
		t.Fatal("cycle still open after pause, want closed — unanswered messages wait for the next round")
	}
}
