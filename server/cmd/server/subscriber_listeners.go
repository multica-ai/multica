package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// autoSubscribeDefaults — when a user has no override in user.preferences
// these defaults decide whether the listener auto-subscribes them.
// Reasons map 1:1 to the strings written to issue_subscriber.reason.
//
// Defaults follow the principle "subscribe me only when it's my task":
//   - creator / assignee  → on  (you own it, follow it)
//   - mentioned / commenter → off (opt-in via Settings → Notifications)
//
// Keep this map in sync with AUTO_SUBSCRIBE_DEFAULTS in
// packages/core/notifications/auto-subscribe.ts.
var autoSubscribeDefaults = map[string]bool{
	"creator":   true,
	"assignee":  true,
	"mentioned": false,
	"commenter": false,
}

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
		maybeAddSubscriber(bus, queries, e.WorkspaceID, issue.ID, issue.CreatorType, issue.CreatorID, "creator")

		// Subscribe the assignee if exists and different from creator
		if issue.AssigneeType != nil && issue.AssigneeID != nil &&
			!(*issue.AssigneeType == issue.CreatorType && *issue.AssigneeID == issue.CreatorID) {
			maybeAddSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee")
		}

		// Subscribe @mentioned users in description
		if issue.Description != nil && *issue.Description != "" {
			for _, m := range parseMentions(*issue.Description) {
				maybeAddSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned")
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
				maybeAddSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee")
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
						maybeAddSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned")
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
		var issueID, authorType, authorID string
		if comment, ok := payload["comment"].(handler.CommentResponse); ok {
			issueID = comment.IssueID
			authorType = comment.AuthorType
			authorID = comment.AuthorID
		} else if commentMap, ok := payload["comment"].(map[string]any); ok {
			issueID, _ = commentMap["issue_id"].(string)
			authorType, _ = commentMap["author_type"].(string)
			authorID, _ = commentMap["author_id"].(string)
		} else {
			return
		}
		if issueID == "" || authorID == "" {
			return
		}

		maybeAddSubscriber(bus, queries, e.WorkspaceID, issueID, authorType, authorID, "commenter")
	})
}

// shouldAutoSubscribe returns whether a user should be auto-subscribed for a
// given reason. Agents always subscribe (they have no preferences UI and rely
// on subscription for their own task pickup). Members consult
// user.preferences.auto_subscribe.<reason>, falling back to autoSubscribeDefaults.
func shouldAutoSubscribe(ctx context.Context, queries *db.Queries, userType, userID, reason string) bool {
	if userType != "member" {
		return true
	}
	def, known := autoSubscribeDefaults[reason]
	if !known {
		// Unknown reason — preserve old "always subscribe" behavior so future
		// reasons don't silently turn into opt-in by accident.
		return true
	}
	prefs, err := queries.GetUserPreferences(ctx, parseUUID(userID))
	if err != nil || len(prefs) == 0 {
		return def
	}
	var blob map[string]any
	if err := json.Unmarshal(prefs, &blob); err != nil {
		slog.Warn("preferences unmarshal failed for auto_subscribe", "user_id", userID, "error", err)
		return def
	}
	block, _ := blob["auto_subscribe"].(map[string]any)
	if block == nil {
		return def
	}
	override, present := block[reason]
	if !present {
		return def
	}
	v, ok := override.(bool)
	if !ok {
		return def
	}
	return v
}

// maybeAddSubscriber wraps addSubscriber with the user's auto-subscribe
// preference check. If the user has opted out of this reason, no row is
// inserted and no subscriber:added event is published.
func maybeAddSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, userType, userID, reason string) {
	if !shouldAutoSubscribe(context.Background(), queries, userType, userID, reason) {
		return
	}
	addSubscriber(bus, queries, workspaceID, issueID, userType, userID, reason)
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
