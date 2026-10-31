package statusnotify

import (
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
)

// ExtractChange pulls a Change out of an EventIssueUpdated event, reporting
// false when the event is not a status transition.
//
// The payload shape mirrors what notification_listeners.go already consumes
// for in-app notifications, so this reads the same fields rather than
// inventing a second contract for the same event.
func ExtractChange(e events.Event) (Change, bool) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return Change{}, false
	}
	statusChanged, _ := payload["status_changed"].(bool)
	if !statusChanged {
		return Change{}, false
	}
	issue, ok := payload["issue"].(handler.IssueResponse)
	if !ok {
		return Change{}, false
	}

	// StatusCategory carries omitempty, and its own doc comment tells
	// consumers to fall back to Status rather than assume a blank means
	// anything. A blank here would otherwise match no subscription and drop
	// the notification silently.
	category := issue.StatusCategory
	if category == "" {
		category = issue.Status
	}

	change := Change{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Identifier:  issue.Identifier,
		Title:       issue.Title,
		Status:      issue.Status,
		Category:    category,
	}
	if issue.Description != nil {
		change.Description = *issue.Description
	}
	if prev, ok := payload["prev_status"].(string); ok {
		change.PrevStatus = prev
	} else if prev, ok := payload["prev_status"].(*string); ok && prev != nil {
		change.PrevStatus = *prev
	}
	// WorkspaceID on the issue is authoritative, but fall back to the event's
	// own scope when the payload omitted it.
	if change.WorkspaceID == "" {
		change.WorkspaceID = e.WorkspaceID
	}
	return change, true
}
