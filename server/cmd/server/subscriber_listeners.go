package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/autosubscribe"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerSubscriberListeners wires up event bus listeners that auto-subscribe
// relevant users to issues. This ensures creators, assignees, and commenters
// are automatically tracked as issue subscribers.
func registerSubscriberListeners(bus *events.Bus, queries *db.Queries) {
	// issue:created — subscribe creator + assignee (if different)
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Issues created via handler use IssueResponse; autopilot-created issues
		// use map[string]any (see service/autopilot.go → issueToMap).
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		// Subscribe the creator
		maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issue.ID, issue.CreatorType, issue.CreatorID, "creator", autosubscribe.SourceIssueCreator)

		// Subscribe the assignee if exists and different from creator
		if issue.AssigneeType != nil && issue.AssigneeID != nil &&
			!(*issue.AssigneeType == issue.CreatorType && *issue.AssigneeID == issue.CreatorID) {
			maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee", autosubscribe.SourceIssueAssignee)
		}

		// Subscribe @mentioned users in description
		if issue.Description != nil && *issue.Description != "" {
			for _, m := range parseMentions(*issue.Description) {
				maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned", autosubscribe.SourceIssueDescriptionMention)
			}
		}
	})

	// issue:updated — subscribe new assignee or @mentioned users
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		// Subscribe new assignee if assignee changed
		if assigneeChanged, _ := payload["assignee_changed"].(bool); assigneeChanged {
			if issue.AssigneeType != nil && issue.AssigneeID != nil {
				maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee", autosubscribe.SourceIssueAssignee)
			}
		}

		// Subscribe newly @mentioned users in description
		if descriptionChanged, _ := payload["description_changed"].(bool); descriptionChanged && issue.Description != nil {
			newMentions := parseMentions(*issue.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription, _ := payload["prev_description"].(*string); prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned", autosubscribe.SourceIssueDescriptionMention)
					}
				}
			}
		}
	})

	// comment:created — subscribe the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// Comments created via handler use CommentResponse; agent comments from task.go use map[string]any
		var issueID, authorType, authorID, content string
		if comment, ok := payload["comment"].(handler.CommentResponse); ok {
			issueID = comment.IssueID
			authorType = comment.AuthorType
			authorID = comment.AuthorID
			content = comment.Content
		} else if commentMap, ok := payload["comment"].(map[string]any); ok {
			issueID, _ = commentMap["issue_id"].(string)
			authorType, _ = commentMap["author_type"].(string)
			authorID, _ = commentMap["author_id"].(string)
			content, _ = commentMap["content"].(string)
		} else {
			return
		}
		if issueID == "" || authorID == "" {
			return
		}

		// Platform-authored system comments (MUL-2538 child-done parent notify)
		// have author_type='system' and a zero UUID author. They must NOT
		// add a subscriber row: issue_subscriber.user_type is constrained to
		// ('member','agent'), and a "system" subscriber has no inbox to read
		// anyway. Skip them at the side-effect boundary so the system event
		// stays a pure WS broadcast for the timeline.
		if authorType == "system" {
			return
		}

		maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issueID, authorType, authorID, "commenter", autosubscribe.SourceCommentAuthor)

		for _, m := range parseMentions(content) {
			if m.Type != "member" {
				continue
			}
			maybeAddAutoSubscriber(bus, queries, e.WorkspaceID, issueID, "member", m.ID, "mentioned", autosubscribe.SourceCommentMention)
		}
	})
}

// extractIssueFields normalizes an issue payload that may be either a
// handler.IssueResponse struct (HTTP handler path) or a map[string]any
// (autopilot service path) into a common shape.
func extractIssueFields(v any) (handler.IssueResponse, bool) {
	if issue, ok := v.(handler.IssueResponse); ok {
		return issue, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return handler.IssueResponse{}, false
	}
	issue := handler.IssueResponse{}
	issue.ID, _ = m["id"].(string)
	issue.WorkspaceID, _ = m["workspace_id"].(string)
	issue.CreatorType, _ = m["creator_type"].(string)
	issue.CreatorID, _ = m["creator_id"].(string)
	issue.AssigneeType, _ = m["assignee_type"].(*string)
	issue.AssigneeID, _ = m["assignee_id"].(*string)
	issue.Description, _ = m["description"].(*string)
	if issue.ID == "" || issue.CreatorID == "" {
		return handler.IssueResponse{}, false
	}
	return issue, true
}

func maybeAddAutoSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, userType, userID, reason string, source autosubscribe.Source) {
	if !autosubscribe.ShouldSubscribe(context.Background(), queries, workspaceID, userType, userID, source) {
		return
	}
	addSubscriber(bus, queries, workspaceID, issueID, userType, userID, reason)
}

// addSubscriber adds a user as an issue subscriber and publishes a
// subscriber:added event for real-time frontend sync.
func addSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, userType, userID, reason string) {
	err := queries.AddIssueSubscriber(context.Background(), db.AddIssueSubscriberParams{
		IssueID:  parseUUID(issueID),
		UserType: userType,
		UserID:   parseUUID(userID),
		Reason:   reason,
	})
	if err != nil {
		slog.Error("failed to add issue subscriber",
			"issue_id", issueID,
			"user_type", userType,
			"user_id", userID,
			"reason", reason,
			"error", err,
		)
		return
	}

	bus.Publish(events.Event{
		Type:        protocol.EventSubscriberAdded,
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"issue_id":  issueID,
			"user_type": userType,
			"user_id":   userID,
			"reason":    reason,
		},
	})
}
