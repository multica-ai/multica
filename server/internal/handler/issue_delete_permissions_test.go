package handler

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueDeletionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, role, actor, creator string
		restricted, allowed        bool
	}{
		{"default member deletes others", "member", "member", "other", false, true},
		{"default cloud PAT deletes others", "owner", "cloud", "other", false, true},
		{"cloud PAT denied admin rights", "admin", "cloud", "other", true, false},
		{"cloud PAT denied owner rights", "owner", "cloud", "other", true, false},
		{"cloud PAT denied member ownership", "member", "cloud", "self", true, false},
		{"cloud PAT denied agent ownership", "owner", "cloud", "owned-agent", true, false},
		{"default agent deletes others", "owner", "agent", "other", false, true},
		{"member deletes own", "member", "member", "self", true, true},
		{"member denied others", "member", "member", "other", true, false},
		{"member deletes own agent issue", "member", "member", "owned-agent", true, true},
		{"member denied other agent issue", "member", "member", "other-agent", true, false},
		{"owner deletes others", "owner", "member", "other", true, true},
		{"admin deletes others", "admin", "member", "other", true, true},
		{"agent deletes own", "member", "agent", "owned-agent", true, true},
		{"agent denied human issue", "owner", "agent", "self", true, false},
		{"agent denied sibling issue", "owner", "agent", "other-agent", true, false},
		{"member denied missing agent", "member", "member", "missing-agent", true, false},
	} {
		for _, batch := range []bool{false, true} {
			t.Run(tc.name+"/batch="+strconv.FormatBool(batch), func(t *testing.T) {
				ws := dbfx.Workspace(t, "deletion policy", "delete-"+uuid.NewString(), testutil.Cols{"restrict_issue_deletion": tc.restricted})
				dbfx.Member(t, ws, testUserID, tc.role)
				agent := dbfx.Agent(t, "own agent", "", testutil.Cols{"workspace_id": ws})
				other := dbfx.User(t, "Other", "delete-"+uuid.NewString()+"@example.test")
				otherAgent := dbfx.Agent(t, "other agent", "", testutil.Cols{"workspace_id": ws, "owner_id": other})
				creatorType, creatorID := "member", testUserID
				switch tc.creator {
				case "other":
					creatorID = other
				case "owned-agent":
					creatorType, creatorID = "agent", agent
				case "other-agent":
					creatorType, creatorID = "agent", otherAgent
				case "missing-agent":
					creatorType, creatorID = "agent", "00000000-0000-4000-8000-000000000001"
				}
				id := dbfx.Issue(t, "delete target", testutil.Cols{"workspace_id": ws, "creator_type": creatorType, "creator_id": creatorID})
				req := withURLParam(newRequest(http.MethodDelete, "/api/issues/"+id, nil), "id", id)
				req.Header.Set("X-Workspace-ID", ws)
				if tc.actor == "agent" {
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", agent)
				} else if tc.actor == "cloud" {
					req.Header.Set("X-Actor-Source", "cloud_pat")
				}
				handler := testHandler.DeleteIssue
				if batch {
					req = newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{id}})
					req.Header.Set("X-Workspace-ID", ws)
					if tc.actor == "agent" {
						req.Header.Set("X-Actor-Source", "task_token")
						req.Header.Set("X-Agent-ID", agent)
					} else if tc.actor == "cloud" {
						req.Header.Set("X-Actor-Source", "cloud_pat")
					}
					handler = testHandler.BatchDeleteIssues
				}
				want := http.StatusForbidden
				if tc.allowed {
					want = http.StatusNoContent
					if batch {
						want = http.StatusOK
					}
				}
				testutil.Call(t, handler, req).Want(want)
				count := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id=$1", id)
				if (count == 0) != tc.allowed {
					t.Fatalf("remaining issues = %d, allowed = %v", count, tc.allowed)
				}
			})
		}
	}
}

func TestIssueDeletionBatchRejectsBeforeCancellingRuns(t *testing.T) {
	ws := dbfx.Workspace(t, "batch deletion", "delete-"+uuid.NewString(), testutil.Cols{"restrict_issue_deletion": true})
	dbfx.Member(t, ws, testUserID, "member")
	other := dbfx.User(t, "Other", "delete-"+uuid.NewString()+"@example.test")
	owned := dbfx.Issue(t, "owned", testutil.Cols{"workspace_id": ws})
	denied := dbfx.Issue(t, "denied", testutil.Cols{"workspace_id": ws, "creator_id": other})
	runtime := dbfx.Runtime(t, "batch runtime", testutil.Cols{"workspace_id": ws})
	agent := dbfx.Agent(t, "batch agent", runtime, testutil.Cols{"workspace_id": ws})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": owned, "runtime_id": runtime})
	req := newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{owned, denied}})
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.BatchDeleteIssues, req).Want(http.StatusForbidden)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id = ANY($1::uuid[])", []string{owned, denied}); n != 2 {
		t.Fatalf("remaining issues = %d", n)
	}
	var status string
	dbfx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", task).Scan(&status)
	if status != "queued" {
		t.Fatalf("denied batch changed run to %s", status)
	}
	// Once the unauthorized entry is removed, the same endpoint remains usable.
	req = newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{owned}})
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.BatchDeleteIssues, req).Want(http.StatusOK)
}
