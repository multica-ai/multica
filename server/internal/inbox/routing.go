package inbox

import "github.com/jackc/pgx/v5/pgtype"

// Recipients choose, per kind of notification, whether it lands in "action",
// in "activity", or nowhere ("muted"). The choice lives in the recipient's
// notification preferences under the kind's key.
//
// Three earlier preference groups each covered two kinds that now default to
// different sections (assignments, comments, agent_activity). Their kinds got
// new keys, and a muted legacy group still mutes both halves until the
// recipient sets the new key, so nobody starts receiving what they had
// silenced.
const (
	RouteAction   = SectionAction
	RouteActivity = SectionActivity
	RouteMuted    = "muted"
	// RouteDefault is what older clients write to unmute a group.
	RouteDefault = "all"
)

type routeKind struct {
	Key     string
	Default string
	// Legacy is the pre-routing group whose "muted" still applies while Key
	// is unset. Empty when the key itself predates routing.
	Legacy string
}

// RouteKinds lists the routable kinds in the order settings render them.
var RouteKinds = []routeKind{
	{Key: "assigned", Default: SectionAction, Legacy: "assignments"},
	{Key: "mentions", Default: SectionAction},
	{Key: "member_comments", Default: SectionAction, Legacy: "comments"},
	{Key: "agent_failures", Default: SectionAction, Legacy: "agent_activity"},
	{Key: "status_changes", Default: SectionActivity},
	{Key: "assignee_changes", Default: SectionActivity, Legacy: "assignments"},
	{Key: "updates", Default: SectionActivity},
	{Key: "agent_comments", Default: SectionActivity, Legacy: "comments"},
}

var routeKindByKey = func() map[string]routeKind {
	m := make(map[string]routeKind, len(RouteKinds))
	for _, k := range RouteKinds {
		m[k.Key] = k
	}
	return m
}()

// RouteKindKey names the routable kind of a notification, or "" when the type
// is not routable (review requests, quick-create and autopilot results,
// reactions...) and always lands in its default section.
func RouteKindKey(notifType string, actorType pgtype.Text) string {
	agent := actorType.Valid && actorType.String == "agent"
	switch notifType {
	case "issue_assigned":
		return "assigned"
	case "mentioned":
		return "mentions"
	case "new_comment":
		if agent {
			return "agent_comments"
		}
		return "member_comments"
	case "task_failed", "agent_blocked":
		return "agent_failures"
	case "status_changed":
		return "status_changes"
	case "unassigned", "assignee_changed":
		return "assignee_changes"
	case "priority_changed", "start_date_changed", "due_date_changed":
		return "updates"
	}
	return ""
}

// Route resolves where a notification goes for a recipient with these
// preferences. ok is false when the recipient muted it.
func Route(prefs map[string]string, notifType string, actorType pgtype.Text) (section string, ok bool) {
	kind, routable := routeKindByKey[RouteKindKey(notifType, actorType)]
	if !routable {
		return DefaultSection(notifType, actorType), true
	}
	switch prefs[kind.Key] {
	case RouteAction:
		return SectionAction, true
	case RouteActivity:
		return SectionActivity, true
	case RouteMuted:
		return "", false
	}
	if kind.Legacy != "" && prefs[kind.Legacy] == RouteMuted {
		return "", false
	}
	return kind.Default, true
}

// IsRouteKey reports whether key is a routable kind's preference key.
func IsRouteKey(key string) bool {
	_, ok := routeKindByKey[key]
	return ok
}
