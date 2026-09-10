package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateIssueProjectIntentJSON(t *testing.T) {
	for _, tc := range []struct {
		body     string
		explicit bool
	}{
		{`{"title":"child"}`, false},
		{`{"title":"child","project_id":null}`, true},
		{`{"title":"child","project_id":"project"}`, true},
	} {
		var req CreateIssueRequest
		if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
			t.Fatal(err)
		}
		if req.projectIDSet != tc.explicit {
			t.Fatalf("%s: explicit = %v", tc.body, req.projectIDSet)
		}
	}
}

func TestIssueCreationUsesSelectedProjectStatusNodes(t *testing.T) {
	seedTestCatalog(t)
	projectID := dbfx.Project(t, "Status selection project")
	var flow issueWorkflowResponse
	testutil.Call(t, testHandler.UpdateProjectIssueWorkflow,
		withURLParam(newRequest(http.MethodPut, "/api/projects/"+projectID+"/issue-workflow", map[string]any{
			"mode": "custom", "spec": map[string]any{
				"api_version": 1, "name": "Editorial", "initial_status": "brief",
				"statuses": []map[string]any{
					{"key": "brief", "name": "Brief", "phase": "backlog", "color": "#123456"},
					{"key": "review", "name": "Editorial review", "phase": "started", "color": "#654321"},
				},
			},
		}), "id", projectID)).Want(http.StatusOK).JSON(&flow)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workflow_id = $1`, flow.Workflow.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, flow.Workflow.ID)
	})
	var reviewID string
	for _, node := range flow.Statuses {
		if node.Name == "Editorial review" {
			reviewID = node.ID
		}
	}
	if reviewID == "" {
		t.Fatal("missing review node")
	}
	create := func(title string, fields map[string]any) IssueResponse {
		t.Helper()
		fields["title"] = title
		var issue IssueResponse
		testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", fields)).Want(http.StatusCreated).JSON(&issue)
		t.Cleanup(func() {
			testutil.Call(t, testHandler.DeleteIssue, withURLParam(newRequest(http.MethodDelete, "/api/issues/"+issue.ID, nil), "id", issue.ID)).Want(http.StatusNoContent)
		})
		return issue
	}
	parent := create("Default to the configured initial node", map[string]any{"project_id": projectID})
	if parent.WorkflowStatusID == nil || *parent.WorkflowStatusID != *flow.Workflow.InitialStatusID {
		t.Fatalf("initial node = %v", parent.WorkflowStatusID)
	}
	selected := create("Select a project node", map[string]any{"project_id": projectID, "workflow_status_id": reviewID})
	if selected.WorkflowStatusID == nil || *selected.WorkflowStatusID != reviewID {
		t.Fatalf("selected node = %v", selected.WorkflowStatusID)
	}
	inherited := create("Inherit parent project when omitted", map[string]any{"parent_issue_id": parent.ID})
	if inherited.ProjectID == nil || *inherited.ProjectID != projectID {
		t.Fatal("omitted project did not inherit")
	}
	cleared := create("Explicitly create without a project", map[string]any{"parent_issue_id": parent.ID, "project_id": nil})
	if cleared.ProjectID != nil || cleared.WorkflowID == nil || *cleared.WorkflowID == flow.Workflow.ID {
		t.Fatal("explicit null inherited the parent workflow")
	}
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": "Foreign status must fail", "project_id": nil, "workflow_status_id": reviewID,
	})).Want(http.StatusConflict)

	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch", map[string]any{
		"issue_ids": []string{parent.ID}, "updates": map[string]any{"workflow_status_id": reviewID},
	})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch", map[string]any{
		"issue_ids": []string{cleared.ID, parent.ID}, "updates": map[string]any{"project_id": nil, "priority": "high"},
	})).Want(http.StatusConflict)
	var unchanged IssueResponse
	testutil.Call(t, testHandler.GetIssue, withURLParam(newRequest(http.MethodGet, "/api/issues/"+cleared.ID, nil), "id", cleared.ID)).Want(http.StatusOK).JSON(&unchanged)
	if unchanged.Priority == "high" {
		t.Fatal("rejected cross-workflow batch partially updated an earlier task")
	}

	originalStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = originalStorage })
	commentID := dbfx.Comment(t, parent.ID, "Create a follow-up for this review")
	var preview sourceContextPreviewResponse
	testutil.Call(t, testHandler.PreviewCommentSubIssue, withURLParam(newRequest(http.MethodGet, "/api/comments/"+commentID+"/sub-issue-preview", nil), "commentId", commentID)).Want(http.StatusOK).JSON(&preview)
	var child IssueResponse
	testutil.Call(t, testHandler.CreateCommentSubIssue, withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/sub-issues", map[string]any{
		"mode": "manual", "capture_token": preview.CaptureToken,
		"issue": map[string]any{"title": "Comment-created review", "project_id": projectID, "workflow_status_id": reviewID},
	}), "commentId", commentID)).Want(http.StatusCreated).JSON(&child)
	t.Cleanup(func() {
		testutil.Call(t, testHandler.DeleteIssue, withURLParam(newRequest(http.MethodDelete, "/api/issues/"+child.ID, nil), "id", child.ID)).Want(http.StatusNoContent)
	})
	if child.WorkflowStatusID == nil || *child.WorkflowStatusID != reviewID {
		t.Fatalf("comment child ignored status node: %#v", child)
	}
}
