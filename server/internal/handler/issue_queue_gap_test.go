package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type queueGapFixture struct {
	parent IssueResponse
	childA IssueResponse
	childB IssueResponse
}

func newQueueGapFixture(t *testing.T, parentMetadata string) queueGapFixture {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "queue-gap parent " + time.Now().Format(time.RFC3339Nano),
		"status":        "in_progress",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	if parentMetadata != "" {
		if _, err := testPool.Exec(context.Background(), `UPDATE issue SET metadata = $2::jsonb WHERE id = $1`, parent.ID, parentMetadata); err != nil {
			t.Fatalf("set parent metadata: %v", err)
		}
	}

	childA := createQueueGapChild(t, parent.ID, "in_review")
	childB := createQueueGapChild(t, parent.ID, "in_progress")

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, parent.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, childA.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, childB.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	return queueGapFixture{parent: parent, childA: childA, childB: childB}
}

func createQueueGapChild(t *testing.T, parentID, status string) IssueResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "queue-gap child " + status + " " + time.Now().Format(time.RFC3339Nano),
		"status":          status,
		"parent_issue_id": parentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child status=%q: expected 201, got %d: %s", status, w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	return child
}

func createQueueGapProject(t *testing.T) ProjectResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "queue-gap project " + time.Now().Format(time.RFC3339Nano),
		"status":    "in_progress",
		"lead_type": "member",
		"lead_id":   testUserID,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, project.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, project.ID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, project.ID)
	})

	return project
}

func createQueueGapProjectIssue(t *testing.T, projectID, status string) IssueResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "queue-gap project issue " + status + " " + time.Now().Format(time.RFC3339Nano),
		"status":     status,
		"project_id": projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project issue status=%q: expected 201, got %d: %s", status, w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode project issue: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = NULL, assignee_id = NULL WHERE id = $1`, issue.ID); err != nil {
		t.Fatalf("clear project issue assignee: %v", err)
	}
	return issue
}

func queueGapInboxItemsOn(t *testing.T, issueID string) []struct {
	Type     string
	Severity string
	Details  []byte
} {
	t.Helper()

	rows, err := testPool.Query(context.Background(), `
		SELECT type, severity, details
		  FROM inbox_item
		 WHERE issue_id = $1
		   AND type = 'queue_gap'
		 ORDER BY created_at ASC
	`, issueID)
	if err != nil {
		t.Fatalf("query queue-gap inbox items: %v", err)
	}
	defer rows.Close()

	var items []struct {
		Type     string
		Severity string
		Details  []byte
	}
	for rows.Next() {
		var item struct {
			Type     string
			Severity string
			Details  []byte
		}
		if err := rows.Scan(&item.Type, &item.Severity, &item.Details); err != nil {
			t.Fatalf("scan queue-gap inbox item: %v", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate queue-gap inbox items: %v", err)
	}
	return items
}

func TestQueueGapAlertWhenLastActiveChildReturnsToReview(t *testing.T) {
	fx := newQueueGapFixture(t, "")

	updateChildStatus(t, fx.childB.ID, "in_review")

	items := queueGapInboxItemsOn(t, fx.parent.ID)
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 queue_gap inbox item on parent, got %d", len(items))
	}
	if items[0].Severity != "action_required" {
		t.Fatalf("expected queue_gap severity action_required, got %q", items[0].Severity)
	}

	var details map[string]any
	if err := json.Unmarshal(items[0].Details, &details); err != nil {
		t.Fatalf("decode queue_gap details: %v", err)
	}
	if details["scope"] != "parent_issue" {
		t.Fatalf("expected parent_issue scope, got %#v", details["scope"])
	}
	if details["trigger_issue_id"] != fx.childB.ID {
		t.Fatalf("expected trigger_issue_id %q, got %#v", fx.childB.ID, details["trigger_issue_id"])
	}

	content, _, parentNull, typeStr := systemCommentOn(t, fx.parent.ID)
	if !parentNull {
		t.Errorf("queue-gap system comment must be top-level")
	}
	if typeStr != "system" {
		t.Errorf("queue-gap comment type should be system, got %q", typeStr)
	}
	for _, want := range []string{"Queue gap detected", "`todo`", "`blocked`", "`done`/`cancelled`"} {
		if !strings.Contains(content, want) {
			t.Errorf("queue-gap comment should contain %q, got: %s", want, content)
		}
	}
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("queue-gap comment must not include %q mention side effect, got: %s", banned, content)
		}
	}
}

func TestQueueGapAlertSkippedWhenAnotherChildStillActive(t *testing.T) {
	fx := newQueueGapFixture(t, "")
	childC := createQueueGapChild(t, fx.parent.ID, "todo")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, childC.ID)
	})

	updateChildStatus(t, fx.childB.ID, "in_review")

	if got := len(queueGapInboxItemsOn(t, fx.parent.ID)); got != 0 {
		t.Fatalf("expected no queue_gap inbox while another child is todo, got %d", got)
	}
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("expected no queue_gap system comment while another child is todo, got %d", got)
	}
}

func TestQueueGapAlertSkippedWhenParentHasWaitingMetadata(t *testing.T) {
	fx := newQueueGapFixture(t, `{"waiting_on":"human publish authorization"}`)

	updateChildStatus(t, fx.childB.ID, "in_review")

	if got := len(queueGapInboxItemsOn(t, fx.parent.ID)); got != 0 {
		t.Fatalf("expected no queue_gap inbox while parent is explicitly waiting, got %d", got)
	}
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Fatalf("expected no queue_gap system comment while parent is explicitly waiting, got %d", got)
	}
}

func TestProjectQueueGapAlertWhenLastRootIssueReturnsToReview(t *testing.T) {
	project := createQueueGapProject(t)
	trigger := createQueueGapProjectIssue(t, project.ID, "in_progress")

	updateChildStatus(t, trigger.ID, "in_review")

	items := queueGapInboxItemsOn(t, trigger.ID)
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 project queue_gap inbox item on trigger issue, got %d", len(items))
	}

	var details map[string]any
	if err := json.Unmarshal(items[0].Details, &details); err != nil {
		t.Fatalf("decode queue_gap details: %v", err)
	}
	if details["scope"] != "project" {
		t.Fatalf("expected project scope, got %#v", details["scope"])
	}
	if details["target_issue_id"] != trigger.ID {
		t.Fatalf("expected target_issue_id %q, got %#v", trigger.ID, details["target_issue_id"])
	}
}

func TestProjectQueueGapAlertSkippedWhenUnassignedRootIssueStillActive(t *testing.T) {
	for _, activeStatus := range []string{"todo", "in_progress"} {
		t.Run(activeStatus, func(t *testing.T) {
			project := createQueueGapProject(t)
			trigger := createQueueGapProjectIssue(t, project.ID, "in_progress")
			active := createQueueGapProjectIssue(t, project.ID, activeStatus)

			updateChildStatus(t, trigger.ID, "in_review")

			if got := len(queueGapInboxItemsOn(t, trigger.ID)); got != 0 {
				t.Fatalf("expected no project queue_gap while unassigned %s issue remains active, got %d", activeStatus, got)
			}
			if got := len(queueGapInboxItemsOn(t, active.ID)); got != 0 {
				t.Fatalf("expected no queue_gap inbox on the unassigned active issue, got %d", got)
			}
		})
	}
}
