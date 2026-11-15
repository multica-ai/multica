package inbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Preference groups a member can mute. The keys are stored in
// notification_preference.preferences and validated by the preferences API.
const (
	GroupAssignments   = "assignments"
	GroupStatusChanges = "status_changes"
	GroupComments      = "comments"
	GroupMentions      = "mentions"
	GroupReactions     = "reactions"
	GroupUpdates       = "updates"
	GroupAgentActivity = "agent_activity"
	GroupNewIssues     = "new_issues"
)

// typeGroups is every inbox type the server writes, mapped to the preference
// group that can mute it. An empty group means the type is always delivered.
// Deliver refuses a type missing here. TestRegistryMatchesFrontendTypes keeps
// InboxItemType in packages/core/types/inbox.ts equal to these types plus
// legacyTypes.
//
// `mentioned` is deliberately apart from `comments`: muting comment volume
// must not silence someone asking for you by name.
var typeGroups = map[string]string{
	"issue_assigned":     GroupAssignments,
	"unassigned":         GroupAssignments,
	"assignee_changed":   GroupAssignments,
	"status_changed":     GroupStatusChanges,
	"new_comment":        GroupComments,
	"mentioned":          GroupMentions,
	"reaction_added":     GroupReactions,
	"priority_changed":   GroupUpdates,
	"start_date_changed": GroupUpdates,
	"due_date_changed":   GroupUpdates,
	"task_failed":        GroupAgentActivity,
	"app_action_result":  GroupAgentActivity,
	"issue_created":      GroupNewIssues,

	"issue_subscribed":         "",
	"quick_create_done":        "",
	"quick_create_failed":      "",
	"quick_create_unconfirmed": "",
	"autopilot_paused":         "",
	"autopilot_quota_exceeded": "",
	"children_done":            "",
	"project_owner_joined":     "",
	// The agent is stopped until the person answers these.
	"app_action_requested":  "",
	"app_action_reminder":   "",
	"app_connect_requested": "",
}

// legacyTypes are types earlier releases wrote and this server no longer
// does. Their rows remain, so clients keep labels and filter groups for them,
// but Deliver refuses them. Retiring a type moves it here rather than
// deleting it.
var legacyTypes = []string{
	"review_requested",
	"task_completed",
	"agent_blocked",
	"agent_completed",
}

// Types returns every inbox type the server delivers, sorted.
func Types() []string {
	types := make([]string, 0, len(typeGroups))
	for t := range typeGroups {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// Group returns the preference group that can mute notifType; "" means it is
// always delivered. ok is false for an unregistered type.
func Group(notifType string) (group string, ok bool) {
	group, ok = typeGroups[notifType]
	return group, ok
}

// Muted reports whether a member's parsed preferences mute notifType. A
// missing key means the default "all": preferences are stored sparse.
func Muted(prefs map[string]string, notifType string) bool {
	group := typeGroups[notifType]
	if group == "" {
		return false
	}
	return prefs[group] == "muted"
}

// Preferences holds the parsed notification preferences of a set of members
// in one workspace, keyed by user ID. A member without a row is absent.
type Preferences map[string]map[string]string

// LoadPreferences reads the preferences of userIDs in one query, for a fanout
// that delivers to many members. A read failure is logged and yields no
// preferences, so the fanout delivers rather than drops. The result is never
// nil, which is how Deliver tells "loaded" from "not loaded".
func LoadPreferences(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, userIDs []pgtype.UUID) Preferences {
	result := Preferences{}
	if len(userIDs) == 0 {
		return result
	}
	rows, err := queries.ListNotificationPreferencesByUsers(ctx, db.ListNotificationPreferencesByUsersParams{
		WorkspaceID: workspaceID,
		UserIds:     userIDs,
	})
	if err != nil {
		slog.Error("failed to load notification preferences", "error", err)
		return result
	}
	for _, row := range rows {
		var prefs map[string]string
		if err := json.Unmarshal(row.Preferences, &prefs); err != nil {
			continue
		}
		result[util.UUIDToString(row.UserID)] = prefs
	}
	return result
}
