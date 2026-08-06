package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type rejectingIssueStatusWorkflowGate struct {
	calls int
}

func (g *rejectingIssueStatusWorkflowGate) BeforeIssueStatusChange(_ context.Context, _ db.Issue, proposed, _, _ string) (string, error) {
	g.calls++
	return proposed, errors.New("complete the final Workflow step first")
}

func TestUpdateIssueUsesStatusWorkflowGateBeforePersisting(t *testing.T) {
	create := httptest.NewRecorder()
	testHandler.CreateIssue(create, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Status Workflow gate", "status": "todo",
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create issue: status=%d body=%s", create.Code, create.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanup)
	})

	gate := &rejectingIssueStatusWorkflowGate{}
	previous := testHandler.IssueStatusWorkflowGate
	testHandler.IssueStatusWorkflowGate = gate
	t.Cleanup(func() { testHandler.IssueStatusWorkflowGate = previous })

	update := httptest.NewRecorder()
	request := withURLParam(newRequest("PUT", "/api/issues/"+created.ID, map[string]any{"status": "done"}), "id", created.ID)
	testHandler.UpdateIssue(update, request)
	if update.Code != http.StatusConflict {
		t.Fatalf("update status=%d body=%s, want conflict", update.Code, update.Body.String())
	}
	if gate.calls != 1 {
		t.Fatalf("gate calls=%d, want 1", gate.calls)
	}
	issueID, err := util.ParseUUID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), issueID)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != "todo" {
		t.Fatalf("persisted status=%q, want todo", issue.Status)
	}
}
