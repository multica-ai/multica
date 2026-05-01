package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Notification routes — destinations for an inbox_item at insert time.
// Decided per (recipient, type) from the recipient's user.preferences blob.
const (
	routeInbox         = "inbox"
	routeNotifications = "notifications"
	routeOff           = "off"
)

// Default routing per notification type when the user has no override.
// Split types (due_date_changed, priority_changed) get .assignee/.follower
// suffixes so the assignee gets a stronger signal than passive subscribers.
var defaultRoutes = map[string]string{
	"issue_assigned":            routeInbox,
	"mentioned":                 routeInbox,
	"task_failed":               routeInbox,
	"unassigned":                routeNotifications,
	"reaction_added":            routeNotifications,
	"new_comment":               routeNotifications,
	"assignee_changed":          routeNotifications,
	"status_changed":            routeNotifications,
	"due_date_changed.assignee": routeInbox,
	"due_date_changed.follower": routeNotifications,
	"priority_changed.assignee": routeInbox,
	"priority_changed.follower": routeNotifications,
}

// splitTypes carry an .assignee / .follower suffix when looking up routing.
var splitTypes = map[string]bool{
	"due_date_changed": true,
	"priority_changed": true,
}

// routeKey resolves the lookup key for a (notifType, isAssignee) pair.
// For non-split types the key is simply the notification type.
// For split types the key carries an .assignee or .follower suffix.
func routeKey(notifType string, isAssignee bool) string {
	if !splitTypes[notifType] {
		return notifType
	}
	if isAssignee {
		return notifType + ".assignee"
	}
	return notifType + ".follower"
}

// resolveRoute returns the destination for a notification destined for
// recipientID. Reads user.preferences.notifications[<key>] and falls back
// to defaultRoutes. Agents never have preferences and always go to the inbox.
func resolveRoute(ctx context.Context, queries *db.Queries, recipientType, recipientID, notifType string, isAssignee bool) string {
	key := routeKey(notifType, isAssignee)

	if recipientType != "member" {
		// Agents have no per-type preferences — always inbox.
		fallback, ok := defaultRoutes[key]
		if !ok {
			return routeInbox
		}
		// Agents shouldn't end up in 'off' or 'notifications' regardless of
		// the default table — keep their existing routing semantics.
		if fallback == routeOff || fallback == routeNotifications {
			return routeInbox
		}
		return fallback
	}

	prefs, err := queries.GetUserPreferences(ctx, parseUUID(recipientID))
	if err != nil || len(prefs) == 0 {
		return defaultRoutes[key]
	}

	var blob map[string]any
	if err := json.Unmarshal(prefs, &blob); err != nil {
		slog.Warn("preferences unmarshal failed", "user_id", recipientID, "error", err)
		return defaultRoutes[key]
	}

	notifBlock, _ := blob["notifications"].(map[string]any)
	if notifBlock == nil {
		return defaultRoutes[key]
	}

	override, _ := notifBlock[key].(string)
	switch override {
	case routeInbox, routeNotifications, routeOff:
		return override
	default:
		return defaultRoutes[key]
	}
}

// issueAssigneeMemberID returns the assignee user_id when the assignee is a
// member, or "" when the issue has no assignee or is assigned to an agent.
func issueAssigneeMemberID(issue db.Issue) string {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return ""
	}
	if issue.AssigneeType.String != "member" {
		return ""
	}
	return util.UUIDToString(issue.AssigneeID)
}

// responseAssigneeMemberID returns the assignee user_id from the JSON-shaped
// IssueResponse when the assignee is a member, "" otherwise.
func responseAssigneeMemberID(assigneeType, assigneeID *string) string {
	if assigneeType == nil || assigneeID == nil {
		return ""
	}
	if *assigneeType != "member" {
		return ""
	}
	return *assigneeID
}
