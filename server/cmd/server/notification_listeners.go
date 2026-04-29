package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mention represents a parsed @mention from markdown content (local alias).
type mention struct {
	Type string // "member", "agent", "issue", or "all"
	ID   string // user_id, agent_id, issue_id, or "all"
}

// statusLabels maps DB status values to human-readable labels for notifications.
var statusLabels = map[string]string{
	"backlog":     "Backlog",
	"todo":        "Todo",
	"in_progress": "In Progress",
	"in_review":   "In Review",
	"done":        "Done",
	"blocked":     "Blocked",
	"cancelled":   "Cancelled",
}

// priorityLabels maps DB priority values to human-readable labels for notifications.
var priorityLabels = map[string]string{
	"urgent": "Urgent",
	"high":   "High",
	"medium": "Medium",
	"low":    "Low",
	"none":   "No priority",
}

func statusLabel(s string) string {
	if l, ok := statusLabels[s]; ok {
		return l
	}
	return s
}

func priorityLabel(p string) string {
	if l, ok := priorityLabels[p]; ok {
		return l
	}
	return p
}

var emptyDetails = []byte("{}")

// parseMentions extracts mentions from markdown content.
// Delegates to the shared util.ParseMentions and converts to the local type.
func parseMentions(content string) []mention {
	parsed := util.ParseMentions(content)
	result := make([]mention, len(parsed))
	for i, m := range parsed {
		result[i] = mention{Type: m.Type, ID: m.ID}
	}
	return result
}

// parentBubbleNotifTypes is the allowlist of inbox notification types that
// bubble up from a sub-issue to subscribers of its parent. Other event types
// only notify subscribers of the sub-issue itself, to keep parent watchers'
// inboxes focused on the signal that matters most: status transitions.
var parentBubbleNotifTypes = map[string]bool{
	"status_changed": true,
}

// notifySubscribers queries the subscriber table for an issue, excludes the
// actor and any extra IDs, and creates inbox items for each remaining member
// subscriber. Publishes an inbox:new event for each notification.
// If the issue has a parent and the notification type is in the bubble
// allowlist, parent issue subscribers are also notified (deduplicated
// against direct subscribers).
func notifySubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	notified := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details)

	// Only a small allowlist of event types bubbles to parent subscribers.
	if !parentBubbleNotifTypes[notifType] {
		return
	}

	// Also notify parent issue subscribers if this is a sub-issue.
	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("failed to get issue for parent notification",
			"issue_id", issueID, "error", err)
		return
	}
	if !issue.ParentIssueID.Valid {
		return
	}

	// Merge already-notified IDs into exclude set for parent subscribers.
	parentExclude := make(map[string]bool, len(exclude)+len(notified))
	for id := range exclude {
		parentExclude[id] = true
	}
	for id := range notified {
		parentExclude[id] = true
	}

	// Query subscribers from the parent issue, but the inbox item still
	// points to the sub-issue so the user navigates to the actual change.
	parentID := util.UUIDToString(issue.ParentIssueID)
	notifyIssueSubscribers(ctx, queries, bus,
		parentID, issueID, issueStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, details)
}

// notifyIssueSubscribers sends inbox notifications to subscribers of
// subscriberIssueID, but creates inbox items pointing to targetIssueID.
// This allows querying subscribers from a parent issue while the notification
// links to the sub-issue where the change actually occurred.
// Returns the set of member IDs that were notified.
func notifyIssueSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	subscriberIssueID string,
	targetIssueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) map[string]bool {
	notified := map[string]bool{}

	subs, err := queries.ListIssueSubscribers(ctx, parseUUID(subscriberIssueID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"issue_id", subscriberIssueID, "error", err)
		return notified
	}

	for _, sub := range subs {
		// Only notify member-type subscribers (not agents)
		if sub.UserType != "member" {
			continue
		}

		subID := util.UUIDToString(sub.UserID)

		// Skip the actor
		if subID == e.ActorID {
			continue
		}

		// Skip any extra excluded IDs
		if exclude[subID] {
			continue
		}

		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(workspaceID),
			RecipientType: "member",
			RecipientID:   sub.UserID,
			Type:          notifType,
			Severity:      severity,
			IssueID:       parseUUID(targetIssueID),
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       parseUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("subscriber notification creation failed",
				"subscriber_id", subID, "type", notifType, "error", err)
			continue
		}

		notified[subID] = true
		resp := inboxItemToResponse(item)
		resp["issue_status"] = issueStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: workspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}

	return notified
}

// notifyDirect creates an inbox item for a specific recipient. Skips if the
// recipient is the actor. Publishes an inbox:new event on success.
func notifyDirect(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	recipientType string,
	recipientID string,
	workspaceID string,
	e events.Event,
	issueID string,
	issueStatus string,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	// Skip if recipient is the actor
	if recipientID == e.ActorID {
		return
	}

	item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: recipientType,
		RecipientID:   parseUUID(recipientID),
		Type:          notifType,
		Severity:      severity,
		IssueID:       parseUUID(issueID),
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText(e.ActorType),
		ActorID:       parseUUID(e.ActorID),
		Details:       details,
	})
	if err != nil {
		slog.Error("direct notification creation failed",
			"recipient_id", recipientID, "type", notifType, "error", err)
		return
	}

	resp := inboxItemToResponse(item)
	resp["issue_status"] = issueStatus
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		Payload:     map[string]any{"item": resp},
	})
}

// notifyMentionedMembers creates inbox items for each @mentioned member,
// excluding the actor and any IDs in the skip set. When an @all mention is
// present, all workspace members are notified (excluding agents).
func notifyMentionedMembers(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	mentions []mention,
	issueID string,
	issueTitle string,
	issueStatus string,
	title string,
	skip map[string]bool,
	details []byte,
) {
	// Collect the set of member IDs to notify.
	recipientIDs := map[string]bool{}

	hasAll := false
	for _, m := range mentions {
		if m.Type == "all" {
			hasAll = true
			continue
		}
		if m.Type == "member" {
			recipientIDs[m.ID] = true
		}
	}

	// If @all is present, expand to all workspace members.
	if hasAll {
		members, err := queries.ListMembers(context.Background(), parseUUID(e.WorkspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", e.WorkspaceID, "error", err)
		} else {
			for _, m := range members {
				recipientIDs[util.UUIDToString(m.UserID)] = true
			}
		}
	}

	for id := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		item, err := queries.CreateInboxItem(context.Background(), db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(id),
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       parseUUID(issueID),
			Title:         title,
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       parseUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("mention inbox creation failed", "mentioned_id", id, "error", err)
			continue
		}
		resp := inboxItemToResponse(item)
		resp["issue_status"] = issueStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: e.WorkspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}
}

// registerNotificationListeners wires up event bus listeners that create inbox
// notifications using the subscriber table. This replaces the old hardcoded
// notification logic from inbox_listeners.go.
//
// NOTE: uses context.Background() because the event bus dispatches synchronously
// within the HTTP request goroutine. Adding per-handler timeouts is a bus-level
// concern — see events.Bus for future improvements.
func registerNotificationListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// issue:created — Direct notification to assignee if assignee != actor
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		// Track who already got notified to avoid duplicates
		skip := map[string]bool{e.ActorID: true}

		// Direct notification to assignee
		if issue.AssigneeType != nil && issue.AssigneeID != nil {
			skip[*issue.AssigneeID] = true
			notifyDirect(ctx, queries, bus,
				*issue.AssigneeType, *issue.AssigneeID,
				issue.WorkspaceID, e, issue.ID, issue.Status,
				"issue_assigned", "action_required",
				issue.Title,
				"",
				emptyDetails,
			)
		}

		// Notify @mentions in description
		if issue.Description != nil && *issue.Description != "" {
			mentions := parseMentions(*issue.Description)
			notifyMentionedMembers(bus, queries, e, mentions, issue.ID, issue.Title, issue.Status,
				issue.Title, skip, emptyDetails)
		}
	})

	// issue:updated — handle assignee changes, status changes, priority, due date
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		statusChanged, _ := payload["status_changed"].(bool)
		descriptionChanged, _ := payload["description_changed"].(bool)
		prevAssigneeType, _ := payload["prev_assignee_type"].(*string)
		prevAssigneeID, _ := payload["prev_assignee_id"].(*string)
		prevDescription, _ := payload["prev_description"].(*string)

		if assigneeChanged {
			// Build structured details for assignee change
			detailsMap := map[string]any{}
			if prevAssigneeType != nil {
				detailsMap["prev_assignee_type"] = *prevAssigneeType
			}
			if prevAssigneeID != nil {
				detailsMap["prev_assignee_id"] = *prevAssigneeID
			}
			if issue.AssigneeType != nil {
				detailsMap["new_assignee_type"] = *issue.AssigneeType
			}
			if issue.AssigneeID != nil {
				detailsMap["new_assignee_id"] = *issue.AssigneeID
			}
			assigneeDetails, _ := json.Marshal(detailsMap)

			// Direct: notify new assignee about assignment
			if issue.AssigneeType != nil && issue.AssigneeID != nil {
				notifyDirect(ctx, queries, bus,
					*issue.AssigneeType, *issue.AssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"issue_assigned", "action_required",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Direct: notify old assignee about unassignment
			if prevAssigneeType != nil && prevAssigneeID != nil && *prevAssigneeType == "member" {
				notifyDirect(ctx, queries, bus,
					"member", *prevAssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"unassigned", "info",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Subscriber: notify remaining subscribers about assignee change,
			// excluding actor, old assignee, and new assignee
			exclude := map[string]bool{}
			if prevAssigneeID != nil {
				exclude[*prevAssigneeID] = true
			}
			if issue.AssigneeID != nil {
				exclude[*issue.AssigneeID] = true
			}
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				exclude, "assignee_changed", "info",
				issue.Title, "",
				assigneeDetails)
		}

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			statusDetails, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   issue.Status,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "status_changed", "info",
				issue.Title, "",
				statusDetails)
		}

		if priorityChanged, _ := payload["priority_changed"].(bool); priorityChanged {
			prevPriority, _ := payload["prev_priority"].(string)
			priorityDetails, _ := json.Marshal(map[string]string{
				"from": prevPriority,
				"to":   issue.Priority,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "priority_changed", "info",
				issue.Title, "",
				priorityDetails)
		}

		if dueDateChanged, _ := payload["due_date_changed"].(bool); dueDateChanged {
			prevDueDateStr := ""
			if prevDueDate, ok := payload["prev_due_date"].(*string); ok && prevDueDate != nil {
				prevDueDateStr = *prevDueDate
			}
			newDueDateStr := ""
			if issue.DueDate != nil {
				newDueDateStr = *issue.DueDate
			}
			dueDateDetails, _ := json.Marshal(map[string]string{
				"from": prevDueDateStr,
				"to":   newDueDateStr,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "due_date_changed", "info",
				issue.Title, "",
				dueDateDetails)
		}

		// Notify NEW @mentions in description
		if descriptionChanged && issue.Description != nil {
			newMentions := parseMentions(*issue.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				var added []mention
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						added = append(added, m)
					}
				}
				skip := map[string]bool{e.ActorID: true}
				notifyMentionedMembers(bus, queries, e, added, issue.ID, issue.Title, issue.Status,
					issue.Title, skip, emptyDetails)
			}
		}
	})

	// comment:created — notify all subscribers except the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// The comment payload can come as handler.CommentResponse from the
		// HTTP handler, or as map[string]any from the agent comment path in
		// task.go. Handle both.
		var issueID, commentID, commentContent string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentID = c.ID
			commentContent = c.Content
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
		default:
			return
		}

		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		commentDetails := emptyDetails
		if commentID != "" {
			commentDetails, _ = json.Marshal(map[string]string{
				"comment_id": commentID,
			})
		}

		notifySubscribers(ctx, queries, bus, issueID, issueStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			issueTitle, commentContent,
			commentDetails)

		// Notify @mentions in comment content.
		mentions := parseMentions(commentContent)
		if len(mentions) > 0 {
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMembers(bus, queries, e, mentions, issueID, issueTitle, issueStatus,
				issueTitle, skip, commentDetails)
		}
	})

	// issue_reaction:added — notify the issue creator
	bus.Subscribe(protocol.EventIssueReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.IssueReactionResponse)
		if !ok {
			return
		}

		creatorType, _ := payload["creator_type"].(string)
		creatorID, _ := payload["creator_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if creatorType == "" || creatorID == "" {
			return
		}

		details, _ := json.Marshal(map[string]string{
			"emoji": reaction.Emoji,
		})

		notifyDirect(ctx, queries, bus,
			creatorType, creatorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// reaction:added — notify the comment author
	bus.Subscribe(protocol.EventReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.ReactionResponse)
		if !ok {
			return
		}

		commentAuthorType, _ := payload["comment_author_type"].(string)
		commentAuthorID, _ := payload["comment_author_id"].(string)
		commentID, _ := payload["comment_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if commentAuthorType == "" || commentAuthorID == "" {
			return
		}

		detailsMap := map[string]string{
			"emoji": reaction.Emoji,
		}
		if commentID != "" {
			detailsMap["comment_id"] = commentID
		}
		details, _ := json.Marshal(detailsMap)

		notifyDirect(ctx, queries, bus,
			commentAuthorType, commentAuthorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// task:completed — no inbox notification (completion is visible from status change)

	// task:failed — notify all subscribers except the agent
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
			slog.Error("task:failed notification: failed to get issue", "issue_id", issueID, "error", err)
			return
		}

		exclude := map[string]bool{}
		if agentID != "" {
			exclude[agentID] = true
		}

		notifySubscribers(ctx, queries, bus, issueID, issue.Status, e.WorkspaceID,
			events.Event{
				Type:        e.Type,
				WorkspaceID: e.WorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
			},
			exclude, "task_failed", "action_required",
			issue.Title, "",
			emptyDetails)
	})
}

// inboxItemToResponse converts a db.InboxItem into a map suitable for
// JSON-serializable event payloads (mirrors handler.inboxToResponse fields).
func inboxItemToResponse(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
}
