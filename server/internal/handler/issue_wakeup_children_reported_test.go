package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChildrenReportedAPIRoundTripAndScheduler(t *testing.T) {
	issue := dbfx.Issue(t, "report coordinator")
	agent := dbfx.Agent(t, "report coordinator", testRuntimeID)
	dbfx.Issue(t, "report worker", testutil.Cols{"parent_issue_id": issue, "status": "in_review", "stage": 1})
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issue)
	body := map[string]any{"agent_id": agent, "kind": "event", "instruction": "Review reports", "condition": map[string]any{"type": "children_reported", "stage": 1}}
	foreignWS := dbfx.Workspace(t, "foreign", "reported-api-foreign")
	foreignIssue := dbfx.Issue(t, "foreign parent", testutil.Cols{"workspace_id": foreignWS})
	testutil.Call(t, testHandler.CreateIssueWakeup, withURLParam(newRequest("POST", "/", body), "id", foreignIssue)).Want(http.StatusNotFound)
	var created db.IssueWakeup
	testutil.Call(t, testHandler.CreateIssueWakeup, withURLParam(newRequest("POST", "/", body), "id", issue)).Want(http.StatusCreated).JSON(&created)
	var listed []db.IssueWakeup
	testutil.Call(t, testHandler.ListIssueWakeups, withURLParam(newRequest("GET", "/", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed) != 1 || string(listed[0].Condition) != string(created.Condition) {
		t.Fatalf("condition did not round-trip: %+v", listed)
	}
	// Editing an instruction must retain the selected aggregate and stage.
	body["instruction"] = "Inspect reports before advancing"
	var updated db.IssueWakeup
	testutil.Call(t, testHandler.CreateIssueWakeup, withURLParams(newRequest("PUT", "/", body), "id", issue, "wakeupID", uuidToString(created.ID))).Want(http.StatusOK).JSON(&updated)
	if updated.Revision <= created.Revision || string(updated.Condition) != string(created.Condition) {
		t.Fatalf("condition changed on edit: %+v", updated)
	}
	dbfx.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", created.ID)
	s := &service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := s.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	dbfx.QueryRow(t, "SELECT payload FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type='condition.met'", created.ID).Scan(&raw)
	var receipt struct {
		Observed map[string]int `json:"observed"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Observed["review_ready"] != 1 || receipt.Observed["closed"] != 0 || receipt.Observed["total"] != 1 {
		t.Fatalf("wrong observation: %s", raw)
	}
	t.Logf("condition.met receipt: %s", raw)
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_id'=$2 AND status='queued'", issue, uuidToString(created.ID)); n != 1 {
		t.Fatalf("queued runs=%d", n)
	}
}

func TestChildrenReportedDoesNotAdvanceSystemStage(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")
	first := createStagedChild(t, fx.parent.ID, 1, "in_progress")
	createStagedChild(t, fx.parent.ID, 2, "backlog")
	updateChildStatus(t, first.ID, "in_review")
	if n := len(childDoneEntries(t, fx.parent.ID)); n != 0 {
		t.Fatalf("in_review advanced the system barrier: %d", n)
	}
	rules := listSystemWakeupsFor(t, fx.parent.ID)
	if len(rules) != 1 || rules[0].Stage == nil || *rules[0].Stage != 1 || rules[0].Remaining != 1 {
		t.Fatalf("review counted as terminal: %+v", rules)
	}
	updateChildStatus(t, first.ID, "done")
	if n := len(childDoneEntries(t, fx.parent.ID)); n != 1 {
		t.Fatalf("terminal stage did not advance: %d", n)
	}
}
