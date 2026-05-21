package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// crossSquadMentionFixture seeds a squad-assigned issue with three agents:
//
//   - leader  : the squad's leader (seeded test agent, has runtime). The
//     fixture INSERTs the squad via raw SQL without an auto squad_member row
//     for the leader, exercising the "leader not in squad_member rows but
//     IS the squad's LeaderID" defensive branch of the cross-squad gate.
//   - worker  : a freshly created agent added to the squad via squad_member.
//     Represents the squad's intended dispatch target.
//   - outsider: a freshly created agent in the SAME workspace but NOT in the
//     squad. Represents the failure mode where the leader, via
//     `multica agent list` (A2A-bypassed), picks a same-role same-workspace
//     agent that lives in a different squad / no squad at all.
//
// Mirrors the structure of squadCommentTriggerFixture but adds the explicit
// worker / outsider split so we can assert who got triggered without relying
// on global side-effects.
type crossSquadMentionFixture struct {
	LeaderID   string
	WorkerID   string
	OutsiderID string
	SquadID    string
	IssueID    string
	Issue      db.Issue
}

func newCrossSquadMentionFixture(t *testing.T) crossSquadMentionFixture {
	t.Helper()
	ctx := context.Background()

	// Seeded leader agent (has runtime via the fixture pool).
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	workerID := createHandlerTestAgent(t, "Cross-Squad Worker", nil)
	outsiderID := createHandlerTestAgent(t, "Cross-Squad Outsider", nil)

	// Raw SQL squad insert — deliberately skips the auto squad_member row for
	// the leader. The cross-squad gate must still treat the leader as
	// in-squad via squad.LeaderID fallback.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Cross-Squad Mention Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Add worker as an explicit squad_member row. Outsider is intentionally
	// NOT added.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, 'agent', $2, 'worker')
	`, squadID, workerID); err != nil {
		t.Fatalf("add worker as squad_member: %v", err)
	}

	// Per-workspace unique issue number — see selfMentionFixture for the same
	// counter-bump pattern.
	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'member', $2, $3, 'squad', $4, $5)
		RETURNING id
	`, testWorkspaceID, testUserID, "cross-squad mention gate", squadID, number).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return crossSquadMentionFixture{
		LeaderID:   leaderID,
		WorkerID:   workerID,
		OutsiderID: outsiderID,
		SquadID:    squadID,
		IssueID:    issueID,
		Issue:      issue,
	}
}

// insertComment inserts a comment on the given issue and returns the loaded row.
func insertCrossSquadComment(t *testing.T, issueID, authorType, authorID, content string) db.Comment {
	t.Helper()
	ctx := context.Background()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, issueID, authorType, authorID, content).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})
	got, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	return got
}

// TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_DropsOutsider covers
// the squad-leader cross-squad failure mode: when an issue is assigned to a
// squad and an agent author @-mentions an agent that is NOT a member of that
// squad, the @mention must NOT enqueue a task. Member-only constraint: agent
// authors are gated; member (human) authors are exempt (see the matching
// test below).
func TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_DropsOutsider(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrossSquadMentionFixture(t)

	content := "[@Outsider](mention://agent/" + fx.OutsiderID + ") please pick this up"
	comment := insertCrossSquadComment(t, fx.IssueID, "agent", fx.LeaderID, content)

	if got := countQueuedOrDispatched(t, fx.OutsiderID, fx.IssueID); got != 0 {
		t.Fatalf("before: expected 0 tasks for outsider, got %d", got)
	}

	testHandler.enqueueMentionedAgentTasks(ctx, fx.Issue, comment, nil, "agent", fx.LeaderID)

	if got := countQueuedOrDispatched(t, fx.OutsiderID, fx.IssueID); got != 0 {
		t.Fatalf("cross-squad @mention from squad-leader agent author MUST be dropped, got %d task(s) for outsider", got)
	}
}

// TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_EnqueuesMember
// is the positive counterpart: when an agent author @mentions an agent that
// IS a member of the issue's squad, the task must still be enqueued. Pins
// down that the gate doesn't over-block.
func TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_EnqueuesMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrossSquadMentionFixture(t)

	content := "[@Worker](mention://agent/" + fx.WorkerID + ") please pick this up"
	comment := insertCrossSquadComment(t, fx.IssueID, "agent", fx.LeaderID, content)

	if got := countQueuedOrDispatched(t, fx.WorkerID, fx.IssueID); got != 0 {
		t.Fatalf("before: expected 0 tasks for worker, got %d", got)
	}

	testHandler.enqueueMentionedAgentTasks(ctx, fx.Issue, comment, nil, "agent", fx.LeaderID)

	if got := countQueuedOrDispatched(t, fx.WorkerID, fx.IssueID); got != 1 {
		t.Fatalf("in-squad @mention from squad-leader agent author MUST be enqueued, got %d task(s) for worker", got)
	}
}

// TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_AllowsLeaderFallback
// proves the defensive branch: when the squad's leader is NOT present in the
// squad_member rows (legacy squad, or a raw-SQL insert that bypassed the
// CreateSquad auto-add), the cross-squad gate still treats squad.LeaderID
// as in-squad. This keeps a leader's own subsequent task chain working on
// legacy squads.
func TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_AllowsLeaderFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrossSquadMentionFixture(t)

	// Sanity: the fixture's leader is NOT in squad_member rows.
	var memberCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM squad_member WHERE squad_id = $1 AND member_type = 'agent' AND member_id = $2
	`, fx.SquadID, fx.LeaderID).Scan(&memberCount); err != nil {
		t.Fatalf("count leader squad_member rows: %v", err)
	}
	if memberCount != 0 {
		t.Fatalf("fixture invariant broken: leader unexpectedly present in squad_member rows (count=%d)", memberCount)
	}

	// A different agent author @mentions the leader (e.g. the worker pinging
	// the leader back). We expect the gate to allow it because the @target
	// is the squad's leader.
	content := "[@Leader](mention://agent/" + fx.LeaderID + ") update — done"
	comment := insertCrossSquadComment(t, fx.IssueID, "agent", fx.WorkerID, content)

	if got := countQueuedOrDispatched(t, fx.LeaderID, fx.IssueID); got != 0 {
		t.Fatalf("before: expected 0 tasks for leader, got %d", got)
	}

	testHandler.enqueueMentionedAgentTasks(ctx, fx.Issue, comment, nil, "agent", fx.WorkerID)

	if got := countQueuedOrDispatched(t, fx.LeaderID, fx.IssueID); got != 1 {
		t.Fatalf("agent → leader @mention MUST be enqueued via leader fallback, got %d task(s) for leader", got)
	}
}

// TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_MixedMentions
// proves the gate is per-mention, not per-comment: when a single comment
// @mentions both an in-squad member and an outsider, the member's task is
// enqueued and the outsider's mention is dropped. Without this guarantee
// a buggy leader could "smuggle" cross-squad dispatch by burying the
// outside agent next to a legitimate squad member in the same comment.
func TestEnqueueMentionedAgentTasks_SquadAssignedAgentAuthor_MixedMentions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrossSquadMentionFixture(t)

	content := "[@Worker](mention://agent/" + fx.WorkerID + ") please own the backend; " +
		"[@Outsider](mention://agent/" + fx.OutsiderID + ") please own the frontend"
	comment := insertCrossSquadComment(t, fx.IssueID, "agent", fx.LeaderID, content)

	testHandler.enqueueMentionedAgentTasks(ctx, fx.Issue, comment, nil, "agent", fx.LeaderID)

	if got := countQueuedOrDispatched(t, fx.WorkerID, fx.IssueID); got != 1 {
		t.Errorf("squad worker mention should enqueue, got %d task(s) for worker", got)
	}
	if got := countQueuedOrDispatched(t, fx.OutsiderID, fx.IssueID); got != 0 {
		t.Errorf("outsider mention should be dropped, got %d task(s) for outsider", got)
	}
}

// TestEnqueueMentionedAgentTasks_SquadAssignedMemberAuthor_BypassesGate locks
// in that the cross-squad gate only constrains agent-authored comments.
// Member (human) authors keep their full @mention agency — they may
// deliberately reach outside the squad (e.g. a workspace owner pinging a
// specialist on a squad-assigned issue).
func TestEnqueueMentionedAgentTasks_SquadAssignedMemberAuthor_BypassesGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrossSquadMentionFixture(t)

	content := "[@Outsider](mention://agent/" + fx.OutsiderID + ") please pick this up"
	comment := insertCrossSquadComment(t, fx.IssueID, "member", testUserID, content)

	if got := countQueuedOrDispatched(t, fx.OutsiderID, fx.IssueID); got != 0 {
		t.Fatalf("before: expected 0 tasks for outsider, got %d", got)
	}

	testHandler.enqueueMentionedAgentTasks(ctx, fx.Issue, comment, nil, "member", testUserID)

	if got := countQueuedOrDispatched(t, fx.OutsiderID, fx.IssueID); got != 1 {
		t.Fatalf("member-authored @mention MUST bypass the cross-squad gate, got %d task(s) for outsider", got)
	}
}
