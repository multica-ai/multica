package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestWakeupOnAnL0SquadIsHeldUntilTheSquadMovesUp checks spec 4.3 for wakeups: an
// issue held by a squad at L0 (Suggest) does not fire a routine wakeup, because a
// wakeup is a routine run. Once the squad is at L1 the held input fires.
func TestWakeupOnAnL0SquadIsHeldUntilTheSquadMovesUp(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()

	squad := f.Squad(t, "L0 squad", agent, testutil.Cols{"autonomy_level": 0})
	if _, err := f.Pool.Exec(ctx, `UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1`, issue, squad); err != nil {
		t.Fatal(err)
	}

	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review latest changes"})
	f.Comment(t, util.UUIDToString(issue), "while the squad is at L0")
	wakeDispatch(t, s, w)

	held, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if held.LastTaskID.Valid {
		t.Fatal("a routine wakeup fired on an issue held by an L0 squad")
	}

	if _, err := f.Pool.Exec(ctx, `UPDATE squad SET autonomy_level = 1 WHERE id = $1`, squad); err != nil {
		t.Fatal(err)
	}
	wakeDispatch(t, s, held)
	after, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastTaskID.Valid {
		t.Fatal("the wakeup did not fire after the squad moved to L1")
	}
}
