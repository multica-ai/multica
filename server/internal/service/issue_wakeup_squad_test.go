package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupSquadLeaderAttribution(t *testing.T) {
	for _, tc := range []struct {
		name       string
		assignee   string
		otherLead  bool
		archived   bool
		rotated    bool
		foreign    bool
		wantLeader bool
	}{
		{name: "current squad leader", assignee: "squad", wantLeader: true},
		{name: "ordinary squad worker", assignee: "squad", otherLead: true},
		{name: "individually assigned leader", assignee: "agent"},
		{name: "archived squad", assignee: "squad", archived: true},
		{name: "leader rotated before dispatch", assignee: "squad", rotated: true},
		{name: "cross workspace squad", assignee: "squad", foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			leader := agent
			if tc.otherLead {
				leader = f.privateAgentOwnedBy(t, f.UserID, "other leader")
			}
			squad := f.Squad(t, "Wakeup squad", leader)
			f.SquadMember(t, squad, "agent", agent)
			if tc.foreign {
				other, _ := newPrincipalFixture(t)
				squad = other.Squad(t, "Foreign squad", agent)
			}
			assignee := squad
			if tc.assignee == "agent" {
				assignee = agent
			}
			f.Exec(t, "UPDATE issue SET assignee_type=$2,assignee_id=$3 WHERE id=$1", issue, tc.assignee, assignee)
			if tc.archived {
				f.Exec(t, "UPDATE squad SET archived_at=now() WHERE id=$1", squad)
			}
			w := wakeCreate(t, f, s, issue, WakeupInput{
				AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "Record the evaluation",
			})
			if tc.rotated {
				next := f.privateAgentOwnedBy(t, f.UserID, "replacement leader")
				f.Exec(t, "UPDATE squad SET leader_id=$2 WHERE id=$1", squad, next)
			}
			f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
			wakeDispatch(t, s, w)
			current, err := f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{
				ID: w.ID, WorkspaceID: w.WorkspaceID,
			})
			if err != nil {
				t.Fatal(err)
			}
			task, err := f.q.GetAgentTask(context.Background(), current.LastTaskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.IsLeaderTask != tc.wantLeader {
				t.Fatalf("is_leader_task=%v, want %v", task.IsLeaderTask, tc.wantLeader)
			}
			wantSquad := pgtype.UUID{}
			if tc.wantLeader {
				wantSquad = parseTestUUID(t, squad)
			}
			if task.SquadID != wantSquad {
				t.Fatalf("squad_id=%s, want %s", util.UUIDToString(task.SquadID), util.UUIDToString(wantSquad))
			}
		})
	}
}
