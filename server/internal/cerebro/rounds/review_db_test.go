package rounds

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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

func mustUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

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
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, f.wsID) })
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Review Owner', 'rounds-review-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&f.ownerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, f.ownerID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, f.wsID, f.ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status) VALUES ($1, 'Review Runtime', 'local', 'claude_code', 'online') RETURNING id`, f.wsID).Scan(&f.runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id) VALUES ($1, 'Review Agent', 'local', $2) RETURNING id`, f.wsID, f.runtimeID).Scan(&f.agentID); err != nil {
		t.Fatal(err)
	}
	f.svc = New(pool, db.New(pool))
	round, err := f.svc.Create(ctx, f.wsID, f.ownerID, "Review")
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
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO inbox_item (workspace_id,recipient_type,recipient_id,type,issue_id,title) VALUES ($1,'member',$2,'new_comment',$3,$4)`, f.wsID, f.ownerID, id, title); err != nil {
		t.Fatal(err)
	}
	return id
}

func cycleHas(t *testing.T, cycle Cycle, issueID pgtype.UUID) bool {
	t.Helper()
	want := util.UUIDToString(issueID)
	for _, item := range cycle.Items {
		if item.IssueID == want {
			return true
		}
	}
	return false
}

func TestStartSnapshotsEligibleMessages(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	ready := f.newIssue(t, "ready")
	running := f.newIssue(t, "running")
	waking := f.newIssue(t, "waking")
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent_task_queue (agent_id,issue_id,runtime_id,status) VALUES ($1,$2,$3,'running')`, f.agentID, running, f.runtimeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO cerebro_agent_wakeup (workspace_id,agent_id,issue_id,prompt,trigger_type,fire_at,state) VALUES ($1,$2,$3,'later','time',now()+interval '1 hour','pending')`, f.wsID, f.agentID, waking); err != nil {
		t.Fatal(err)
	}

	cycle, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if !cycleHas(t, cycle, ready) || cycleHas(t, cycle, running) || cycleHas(t, cycle, waking) {
		t.Fatalf("cycle items = %+v, want only ready issue", cycle.Items)
	}
}

func TestRoundMembersRemainInNormalUnreadCount(t *testing.T) {
	f := newReviewFixture(t)
	f.newIssue(t, "normal unread")

	count, err := db.New(f.pool).CountUnreadInbox(context.Background(), db.CountUnreadInboxParams{
		WorkspaceID: f.wsID, RecipientType: "member", RecipientID: f.ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unread count = %d, want 1 for Round member", count)
	}
}

func TestReplyHandlesCurrentCycle(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	issueID := f.newIssue(t, "answer once")
	cycle, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.MarkHandled(ctx, f.wsID, issueID, f.ownerID); err != nil {
		t.Fatal(err)
	}
	status, err := f.svc.Status(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveCycle == nil || status.ActiveCycle.ID != cycle.ID || status.ActiveCycle.Items[0].HandledAt == nil {
		t.Fatalf("active cycle after reply = %+v, want handled current item", status.ActiveCycle)
	}
}

func TestNextStartSurfacesNewReply(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	issueID := f.newIssue(t, "next round")
	first, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.MarkHandled(ctx, f.wsID, issueID, f.ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO comment (workspace_id,issue_id,author_type,author_id,content) VALUES ($1,$2,'agent',$3,'new reply')`, f.wsID, issueID, f.agentID); err != nil {
		t.Fatal(err)
	}
	status, err := f.svc.Status(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveCycle == nil || status.ActiveCycle.ID != first.ID || status.ActiveCycle.Items[0].HandledAt == nil {
		t.Fatalf("agent reply changed active snapshot: %+v", status.ActiveCycle)
	}
	second, err := f.svc.Start(ctx, f.wsID, f.ownerID, f.roundID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || !cycleHas(t, second, issueID) {
		t.Fatalf("next cycle = %+v, want new snapshot containing issue", second)
	}
}
