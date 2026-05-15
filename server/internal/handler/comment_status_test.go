package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutomaticStatusForMemberComment(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus string
		content       string
		wantStatus    string
		wantChange    bool
	}{
		{
			name:          "review feedback reopens work",
			currentStatus: "in_review",
			content:       "Bitte noch die Tests fixen.",
			wantStatus:    "in_progress",
			wantChange:    true,
		},
		{
			name:          "blocked reply reopens work",
			currentStatus: "blocked",
			content:       "Hier ist der fehlende Zugang.",
			wantStatus:    "in_progress",
			wantChange:    true,
		},
		{
			name:          "german completion marks done",
			currentStatus: "in_review",
			content:       "Die Aufgabe ist erledigt.",
			wantStatus:    "done",
			wantChange:    true,
		},
		{
			name:          "completion negation does not mark done",
			currentStatus: "blocked",
			content:       "Not done yet, please keep working.",
			wantStatus:    "in_progress",
			wantChange:    true,
		},
		{
			name:          "acknowledgement does not reopen review",
			currentStatus: "in_review",
			content:       "Danke!",
			wantChange:    false,
		},
		{
			name:          "cancelled issue stays cancelled",
			currentStatus: "cancelled",
			content:       "done",
			wantChange:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotChange := automaticStatusForMemberComment(tt.currentStatus, tt.content)
			if gotChange != tt.wantChange {
				t.Fatalf("change = %v, want %v", gotChange, tt.wantChange)
			}
			if gotStatus != tt.wantStatus {
				t.Fatalf("status = %q, want %q", gotStatus, tt.wantStatus)
			}
		})
	}
}

func TestCreateCommentAutoTransitionsIssueStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		content    string
		wantStatus string
	}{
		{
			name:       "review feedback reopens issue",
			status:     "in_review",
			content:    "Please revise the edge case handling.",
			wantStatus: "in_progress",
		},
		{
			name:       "blocked reply reopens issue",
			status:     "blocked",
			content:    "The missing token is now available.",
			wantStatus: "in_progress",
		},
		{
			name:       "completion phrase marks issue done",
			status:     "in_review",
			content:    "Die Aufgabe ist erledigt.",
			wantStatus: "done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issueID := createTestIssue(t, "comment status "+tt.name, tt.status, "none")
			t.Cleanup(func() { deleteTestIssue(t, issueID) })

			createMemberComment(t, issueID, tt.content)

			if got := issueStatus(t, issueID); got != tt.wantStatus {
				t.Fatalf("issue status = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestCreateCommentDoneIntentDoesNotTriggerAssignedAgent(t *testing.T) {
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "done intent no trigger",
		"status":        "in_review",
		"priority":      "none",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID); err != nil {
		t.Fatalf("clear initial tasks: %v", err)
	}

	createMemberComment(t, issue.ID, "Die Aufgabe ist erledigt.")

	if got := issueStatus(t, issue.ID); got != "done" {
		t.Fatalf("issue status = %q, want done", got)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`,
		issue.ID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no agent task after done intent comment, got %d", taskCount)
	}
}

func createMemberComment(t *testing.T, issueID, content string) CommentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var comment CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	return comment
}

func issueStatus(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`,
		issueID,
	).Scan(&status); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	return status
}
