package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueWakeupAPIAndTrustedOrigin(t *testing.T) {
	issue := dbfx.Issue(t, "wake api")
	agent := dbfx.Agent(t, "wake api", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN(SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	body := map[string]any{"agent_id": agent, "kind": "at", "after_seconds": 600, "instruction": "check deployment"}
	req := withURLParam(newRequest("POST", "/api/issues/"+issue+"/wakeups", body), "id", issue)
	// An untrusted task header must not get stamped as delegation provenance.
	forged := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issue})
	req.Header.Set("X-Task-ID", forged)
	rec := httptest.NewRecorder()
	testHandler.CreateIssueWakeup(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
	}
	var result db.IssueWakeup
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SourceTaskID.Valid || uuidToString(result.CreatedBy) != testUserID {
		t.Fatal("untrusted source identity")
	}
	req = withURLParam(newRequest("GET", "/", nil), "id", issue)
	rec = httptest.NewRecorder()
	testHandler.ListIssueWakeups(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	outsider := dbfx.User(t, "wake outsider", "wake-outside@multica.test")
	dbfx.Member(t, testWorkspaceID, outsider, "member")
	// A task-scoped caller must use the initiating human's rights, never the
	// runtime owner's rights, even when registering a wakeup for itself.
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$2,accountable_user_id=$2,status='running',started_at=now() WHERE id=$1", forged, outsider)
	req = withURLParam(newRequest("POST", "/", map[string]any{"kind": "at", "after_seconds": 600, "instruction": "check"}), "id", issue)
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", forged)
	req.Header.Set("X-Actor-Source", "task_token")
	rec = httptest.NewRecorder()
	testHandler.CreateIssueWakeup(rec, req)
	if rec.Code != 403 {
		t.Fatalf("borrowed runtime owner permission: %d %s", rec.Code, rec.Body.String())
	}
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), result.ID, parseUUID(outsider)); err != service.ErrWakeupForbidden {
		t.Fatalf("other member disabled: %v", err)
	}
	if _, err := svc.Disable(context.Background(), parseUUID(issue), result.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
}
