package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Background runs (wakeups, the sub-issue rule, app decisions, delegated
// failure recovery) start without a person clicking anything, so the private
// project rule (MUL-8472) has to hold where they are created: the run's human
// and the agent's owner must both see the issue's project.

// privateRunFixture: a regular member who owns a private agent, and an issue
// in a private project. inProject decides whether the member is a project
// member; the workspace owner (f.UserID) is not a project member either way.
type privateRunFixture struct {
	principalFixture
	member  string
	agent   string
	project string
	issue   pgtype.UUID
}

func newPrivateRunFixture(t *testing.T, inProject bool) privateRunFixture {
	t.Helper()
	f, _ := newPrincipalFixture(t)
	if err := f.q.SeedIssueStatusEntries(context.Background(), parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	member := f.member(t, "pp-run")
	project := f.Project(t, "pp-run secret project", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
	if inProject {
		f.InsertNoID(t, "project_member", testutil.Cols{
			"workspace_id": f.WorkspaceID, "project_id": project, "user_id": member,
		}, "project_id = $1 AND user_id = $2", project, member)
	}
	agent := f.privateAgentOwnedBy(t, member, "pp-run")
	issue := f.Issue(t, "pp-run secret issue", testutil.Cols{"project_id": project, "creator_id": member})
	f.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	return privateRunFixture{principalFixture: f, member: member, agent: agent, project: project, issue: parseTestUUID(t, issue)}
}

func (f privateRunFixture) leaveProject(t *testing.T) {
	t.Helper()
	f.Exec(t, "DELETE FROM project_member WHERE project_id=$1 AND user_id=$2", f.project, f.member)
}

func (f privateRunFixture) issueRuns(t *testing.T) int {
	t.Helper()
	return f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", f.issue)
}

func TestIssueWakeupPrivateProjectNeedsTheRulesCreator(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		removed  bool
		wantRuns int
	}{
		{"project member", false, 1},
		{"removed from the project", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPrivateRunFixture(t, true)
			s := &IssueWakeupService{Tasks: f.svc.TaskSvc}
			w, err := s.Create(ctx, f.issue, parseTestUUID(t, f.member), pgtype.UUID{}, WakeupInput{
				AgentID: f.agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review",
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.removed {
				f.leaveProject(t)
			}
			f.Comment(t, util.UUIDToString(f.issue), "trigger")
			wakeDispatch(t, s, w)
			if n := wakeRuns(t, f.principalFixture, w.ID); n != tc.wantRuns {
				t.Fatalf("wakeup runs = %d, want %d", n, tc.wantRuns)
			}
			got, err := f.q.LocklessWakeup(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.removed && (got.Enabled || !got.DisabledAt.Valid) {
				t.Fatalf("rule of a creator outside the project stays on: enabled=%t disabled_at=%v", got.Enabled, got.DisabledAt.Valid)
			}
			// A once rule switches itself off after firing; only the
			// platform sets disabled_at.
			if !tc.removed && got.DisabledAt.Valid {
				t.Fatalf("rule of a project member was disabled: %q", got.LastError.String)
			}
		})
	}
}

// A wakeup run queued while its creator could see the project is refused at
// claim once they cannot, like a creator who lost the agent.
func TestIssueWakeupClaimRechecksPrivateProject(t *testing.T) {
	ctx := context.Background()
	f := newPrivateRunFixture(t, true)
	s := &IssueWakeupService{Tasks: f.svc.TaskSvc}
	w, err := s.Create(ctx, f.issue, parseTestUUID(t, f.member), pgtype.UUID{}, WakeupInput{
		AgentID: f.agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "Review",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.Comment(t, util.UUIDToString(f.issue), "trigger")
	wakeDispatch(t, s, w)
	got, err := f.q.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckClaim(ctx, task); err != nil {
		t.Fatalf("project member's wakeup run refused at claim: %v", err)
	}
	f.leaveProject(t)
	if err := s.CheckClaim(ctx, task); !errors.Is(err, ErrWakeupForbidden) {
		t.Fatalf("claim after leaving the project = %v, want ErrWakeupForbidden", err)
	}
}

// The workspace owner sees every private project without a project_member row.
func TestIssueWakeupPrivateProjectWorkspaceOwner(t *testing.T) {
	f := newPrivateRunFixture(t, false)
	s := &IssueWakeupService{Tasks: f.svc.TaskSvc}
	ownerAgent := f.privateAgentOwnedBy(t, f.UserID, "pp-run-owner")
	w, err := s.Create(context.Background(), f.issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, WakeupInput{
		AgentID: ownerAgent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.Comment(t, util.UUIDToString(f.issue), "trigger")
	wakeDispatch(t, s, w)
	if n := wakeRuns(t, f.principalFixture, w.ID); n != 1 {
		t.Fatalf("owner's wakeup runs = %d, want 1", n)
	}
}

// The sub-issue rule wakes the parent's assignee as the issue's human; an
// assignee whose owner is outside the project is recorded, not run.
func TestChildDoneWakeupPrivateProjectNeedsTheAgentOwner(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		inProject bool
		wantRuns  int
	}{
		{"agent owner in the project", true, 1},
		{"agent owner outside the project", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPrivateRunFixture(t, tc.inProject)
			s := &IssueWakeupService{Tasks: f.svc.TaskSvc}
			// The parent's creator is the workspace owner, so the agent's
			// owner is the only principal the project can stop.
			f.Exec(t, "UPDATE issue SET status='in_progress',assignee_type='agent',assignee_id=$2,creator_id=$3 WHERE id=$1", f.issue, f.agent, f.UserID)
			child := f.Issue(t, "pp-run child", testutil.Cols{"parent_issue_id": f.issue, "project_id": f.project, "status": "in_progress"})
			f.Cleanup(t, "DELETE FROM issue_child_event WHERE parent_id=$1", f.issue)
			if err := s.ProcessChildEvents(ctx, f.issue); err != nil {
				t.Fatal(err)
			}
			f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", child)
			if err := s.ProcessChildEvents(ctx, f.issue); err != nil {
				t.Fatal(err)
			}
			if n := f.issueRuns(t); n != tc.wantRuns {
				t.Fatalf("runs on the parent = %d, want %d", n, tc.wantRuns)
			}
			if tc.wantRuns == 0 {
				n := f.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id=$1
					AND details->>'outcome'='none' AND details->>'reason'=$2`, f.issue, string(dispatch.ReasonProjectAccessBlocked))
				if n != 1 {
					t.Fatalf("blocked timeline entries = %d, want 1", n)
				}
			}
		})
	}
}

func TestAppDecisionRunPrivateProjectNeedsTheDecider(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		removed bool
	}{
		{"project member", false},
		{"removed from the project", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPrivateRunFixture(t, true)
			if tc.removed {
				f.leaveProject(t)
			}
			err := f.svc.TaskSvc.EnqueueAppDecisionRun(ctx, f.issue, parseTestUUID(t, f.agent), parseTestUUID(t, f.member), dbid.NewV7(), "approved")
			runs := f.issueRuns(t)
			if tc.removed {
				if !errors.Is(err, ErrProjectAccessDenied) || runs != 0 {
					t.Fatalf("decision run outside the project: err=%v runs=%d, want ErrProjectAccessDenied and 0", err, runs)
				}
				return
			}
			if err != nil || runs != 1 {
				t.Fatalf("decision run for a project member: err=%v runs=%d, want 1", err, runs)
			}
		})
	}
}

// Recovery waits while the coordinator's human is outside the project and
// resumes once they are back.
func TestDelegatedFailureRecoveryPrivateProjectNeedsTheHuman(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	fx := testutil.New(f.pool, f.workspaceID, f.userID)
	fx.Exec(t, "UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2", f.workspaceID, f.userID)
	project := fx.Project(t, "recovery secret project", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
	fx.Exec(t, "UPDATE issue SET project_id=$2 WHERE id=$1", f.issueID, project)

	failedID := f.insertWorkerTask(t, "running", "comment", 1, 2)
	if _, err := svc.FailTask(ctx, failedID, "worker failed", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	if n := f.recoveryTasks(t, failedID); n != 0 {
		t.Fatalf("recovery runs outside the project = %d, want 0", n)
	}
	var signal pgtype.UUID
	fx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id=$1 AND type='progress_update'`, failedID).Scan(&signal)
	if !f.pending(t, svc, signal) {
		t.Fatal("refused recovery settled its signal")
	}

	fx.InsertNoID(t, "project_member", testutil.Cols{
		"workspace_id": f.workspaceID, "project_id": project, "user_id": f.userID,
	}, "project_id = $1 AND user_id = $2", project, f.userID)
	sweepDelegatedFailureRecoveries(t, svc)
	if n := f.recoveryTasks(t, failedID); n != 1 {
		t.Fatalf("recovery runs once back in the project = %d, want 1", n)
	}
}
