package handler

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// An open Secretary request past its due date is listed. A request that is done,
// not yet due, or filed outside the Secretary desk is not.
func TestDigest_SecretaryRequestPastDeadlineIsListed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newDigestEnv(t)
	desk := dbfx.Project(t, "Secretary desk", testutil.Cols{"workspace_id": e.WorkspaceID})
	secretary := e.AgentID

	overdue := dbfx.Issue(t, "Launch assessment request", testutil.Cols{
		"workspace_id":  e.WorkspaceID,
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   secretary,
		"project_id":    desk,
		"due_date":      testutil.Raw("now() - interval '3 days'"),
	})
	dbfx.Issue(t, "Answered request", testutil.Cols{
		"workspace_id":  e.WorkspaceID,
		"status":        "done",
		"assignee_type": "agent",
		"assignee_id":   secretary,
		"project_id":    desk,
		"due_date":      testutil.Raw("now() - interval '3 days'"),
	})
	dbfx.Issue(t, "Not yet due", testutil.Cols{
		"workspace_id":  e.WorkspaceID,
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   secretary,
		"project_id":    desk,
		"due_date":      testutil.Raw("now() + interval '3 days'"),
	})
	other := dbfx.Project(t, "Unrelated project", testutil.Cols{"workspace_id": e.WorkspaceID})
	dbfx.Issue(t, "Past due outside the desk", testutil.Cols{
		"workspace_id":  e.WorkspaceID,
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   secretary,
		"project_id":    other,
		"due_date":      testutil.Raw("now() - interval '3 days'"),
	})

	d := e.digest(t, time.Now().Add(-24*time.Hour))
	if len(d.RequestsPastDeadline) != 1 {
		t.Fatalf("requests past deadline = %+v, want exactly the open overdue request", d.RequestsPastDeadline)
	}
	got := d.RequestsPastDeadline[0]
	if got.IssueID != overdue || got.DaysOverdue < 2 {
		t.Fatalf("request past deadline = %+v, want issue %s overdue at least 2 days", got, overdue)
	}
}
