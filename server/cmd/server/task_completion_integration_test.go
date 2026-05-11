package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCompleteTaskAutoInReviewPublishesStatusChange(t *testing.T) {
	bus := events.New()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, nil, nil, bus)

	var issueUpdatedEvents []events.Event
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		issueUpdatedEvents = append(issueUpdatedEvents, e)
	})

	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Complete task auto in_review", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	clearTasks(t, issueID)

	taskID := insertRunningTask(t, agentID, issueID)
	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "Travail terminé"})
	if err != nil {
		t.Fatalf("marshal task result: %v", err)
	}
	if _, err := taskSvc.CompleteTask(context.Background(), util.ParseUUID(taskID), result, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	issue, err := queries.GetIssue(context.Background(), util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Status != "in_review" {
		t.Fatalf("expected issue status 'in_review', got %q", issue.Status)
	}

	if len(issueUpdatedEvents) != 1 {
		t.Fatalf("expected 1 issue.updated event after completion, got %d", len(issueUpdatedEvents))
	}
	payload, ok := issueUpdatedEvents[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected issue.updated payload to be map[string]any, got %T", issueUpdatedEvents[0].Payload)
	}
	if statusChanged, _ := payload["status_changed"].(bool); !statusChanged {
		t.Fatal("expected status_changed=true in issue.updated payload")
	}
	issuePayload, ok := payload["issue"].(map[string]any)
	if !ok {
		t.Fatalf("expected issue payload to be map[string]any, got %T", payload["issue"])
	}
	if got, _ := issuePayload["status"].(string); got != "in_review" {
		t.Fatalf("expected published issue status 'in_review', got %q", got)
	}
}

func TestCompleteTaskKeepsNonActiveIssueStatus(t *testing.T) {
	tests := []string{"backlog", "blocked"}

	for _, status := range tests {
		t.Run(status, func(t *testing.T) {
			bus := events.New()
			queries := db.New(testPool)
			taskSvc := service.NewTaskService(queries, nil, nil, bus)

			var issueUpdatedEvents []events.Event
			bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
				issueUpdatedEvents = append(issueUpdatedEvents, e)
			})

			agentID := getAgentID(t)
			issueID := createIssueAssignedToAgent(t, "Complete task preserves "+status, agentID)
			t.Cleanup(func() {
				clearTasks(t, issueID)
				resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
				resp.Body.Close()
			})

			clearTasks(t, issueID)
			resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{"status": status})
			resp.Body.Close()

			taskID := insertRunningTask(t, agentID, issueID)
			result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "Travail terminé"})
			if err != nil {
				t.Fatalf("marshal task result: %v", err)
			}
			if _, err := taskSvc.CompleteTask(context.Background(), util.ParseUUID(taskID), result, "", ""); err != nil {
				t.Fatalf("CompleteTask: %v", err)
			}

			issue, err := queries.GetIssue(context.Background(), util.ParseUUID(issueID))
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if issue.Status != status {
				t.Fatalf("expected issue status %q to be preserved, got %q", status, issue.Status)
			}
			if len(issueUpdatedEvents) != 0 {
				t.Fatalf("expected no issue.updated event for preserved %q status, got %d", status, len(issueUpdatedEvents))
			}
		})
	}
}

func TestCompleteTaskDoesNotAdvanceWhileAnotherIssueTaskIsActive(t *testing.T) {
	bus := events.New()
	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, nil, nil, bus)

	var issueUpdatedEvents []events.Event
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		issueUpdatedEvents = append(issueUpdatedEvents, e)
	})

	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Complete task with follow-up still queued", agentID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	clearTasks(t, issueID)
	resp := authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
	resp.Body.Close()

	runningTaskID := insertRunningTask(t, agentID, issueID)
	_ = insertTaskWithStatus(t, agentID, issueID, "queued")

	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "Travail terminé"})
	if err != nil {
		t.Fatalf("marshal task result: %v", err)
	}
	if _, err := taskSvc.CompleteTask(context.Background(), util.ParseUUID(runningTaskID), result, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	issue, err := queries.GetIssue(context.Background(), util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Status != "in_progress" {
		t.Fatalf("expected issue status 'in_progress' to be preserved while another task is active, got %q", issue.Status)
	}
	if len(issueUpdatedEvents) != 0 {
		t.Fatalf("expected no issue.updated event while another task is active, got %d", len(issueUpdatedEvents))
	}
}
