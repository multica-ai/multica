package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerWebhookListeners wires up event bus listeners that deliver outbound
// webhooks when action_required events occur. This implements issue #1020.
//
// Triggers:
//   - issue:created (with assignee) → issue_assigned
//   - issue:updated (status → in_review) → status_changed
//   - task:failed → task_failed
//   - comment:created (by agent with @mention) → new_comment
func registerWebhookListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()
	webhookSvc := service.NewWebhookService(queries)

	// issue:created → deliver webhook when an issue is assigned
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		if issue.AssigneeID == nil {
			return
		}

		webhookSvc.DeliverToWorkspace(ctx, parseUUID(e.WorkspaceID), "issue_assigned", map[string]any{
			"issue_id":      issue.ID,
			"issue_title":   issue.Title,
			"issue_status":  issue.Status,
			"assignee_type": issue.AssigneeType,
			"assignee_id":   issue.AssigneeID,
			"actor_type":    e.ActorType,
			"actor_id":      e.ActorID,
		})
	})

	// issue:updated → deliver webhook on status changes (especially in_review)
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		statusChanged, _ := payload["status_changed"].(bool)
		if !statusChanged {
			return
		}

		prevStatus, _ := payload["prev_status"].(string)

		webhookSvc.DeliverToWorkspace(ctx, parseUUID(e.WorkspaceID), "status_changed", map[string]any{
			"issue_id":     issue.ID,
			"issue_title":  issue.Title,
			"prev_status":  prevStatus,
			"new_status":   issue.Status,
			"actor_type":   e.ActorType,
			"actor_id":     e.ActorID,
		})
	})

	// task:failed → deliver webhook when an agent task fails
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		if issueID == "" {
			return
		}

		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			slog.Error("webhook: task:failed — failed to get issue",
				"issue_id", issueID, "error", err)
			return
		}

		errorMsg, _ := payload["error"].(string)

		webhookSvc.DeliverToWorkspace(ctx, issue.WorkspaceID, "task_failed", map[string]any{
			"issue_id":    issueID,
			"issue_title": issue.Title,
			"agent_id":    agentID,
			"error":       errorMsg,
			"actor_type":  e.ActorType,
			"actor_id":    e.ActorID,
		})
	})

	// comment:created → deliver webhook for agent comments
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		if e.ActorType != "agent" {
			return
		}

		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		var issueID, commentContent string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentContent = c.Content
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentContent, _ = c["content"].(string)
		default:
			return
		}

		issueTitle, _ := payload["issue_title"].(string)

		webhookSvc.DeliverToWorkspace(ctx, parseUUID(e.WorkspaceID), "agent_comment", map[string]any{
			"issue_id":    issueID,
			"issue_title": issueTitle,
			"content":     commentContent,
			"actor_type":  e.ActorType,
			"actor_id":    e.ActorID,
		})
	})

	slog.Info("webhook listeners registered")
}

func parseUUIDForWebhook(s string) interface{} {
	return util.ParseUUID(s)
}
