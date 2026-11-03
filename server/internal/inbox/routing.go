package inbox

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

// Preference keys of the routable kinds a comment can fall under.
const (
	RouteKeyMentions                 = "mentions"
	RouteKeyCommentsWithoutMentions  = "comments_without_mentions"
	RouteKeyCommentsMentioningOthers = "comments_mentioning_others"
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
	{Key: RouteKeyMentions, Default: SectionAction},
	{Key: RouteKeyCommentsWithoutMentions, Default: SectionAction, Legacy: "comments"},
	{Key: "agent_failures", Default: SectionAction, Legacy: "agent_activity"},
	{Key: "status_changes", Default: SectionActivity},
	{Key: "assignee_changes", Default: SectionActivity, Legacy: "assignments"},
	{Key: "updates", Default: SectionActivity},
	{Key: RouteKeyCommentsMentioningOthers, Default: SectionActivity, Legacy: "comments"},
}

var routeKindByKey = func() map[string]routeKind {
	m := make(map[string]routeKind, len(RouteKinds))
	for _, k := range RouteKinds {
		m[k.Key] = k
	}
	return m
}()

// commentToRecipientKind routes a comment that @mentions the recipient. It
// follows the recipient's mentions choice, since the same comment also sends
// them a "mentioned" notification and the two should land together. Unlike
// the "mentioned" kind it keeps the legacy comments mute: before routing, a
// recipient who muted comments got only the mention for such a comment.
var commentToRecipientKind = routeKind{Key: RouteKeyMentions, Default: SectionAction, Legacy: "comments"}

// CommentAudience is who a comment is written to, as one recipient sees it.
// Only people count as an audience: @member, @agent, @squad and @all. Issue
// and project links do not.
type CommentAudience int

const (
	// CommentToNobody: the comment @mentions nobody. It is usually a reply
	// to whoever is following the conversation, so it waits on them.
	CommentToNobody CommentAudience = iota
	// CommentToRecipient: the comment @mentions the recipient, directly,
	// through a squad they belong to, or through @all. This wins when the
	// comment also @mentions others.
	CommentToRecipient
	// CommentToOthers: the comment @mentions someone, and not the recipient.
	CommentToOthers
)

// RouteKindKey names the routable kind of a notification, or "" when the type
// is not routable (review requests, quick-create and autopilot results,
// reactions...) and always lands in its default section. A new_comment routed
// without an audience is treated as written to nobody; RouteComment is the
// audience-aware entry point.
func RouteKindKey(notifType string) string {
	switch notifType {
	case "issue_assigned":
		return "assigned"
	case "mentioned":
		return RouteKeyMentions
	case "new_comment":
		return RouteKeyCommentsWithoutMentions
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
func Route(prefs map[string]string, notifType string) (section string, ok bool) {
	kind, routable := routeKindByKey[RouteKindKey(notifType)]
	if !routable {
		return DefaultSection(notifType), true
	}
	return routeTo(prefs, kind)
}

// RouteComment resolves where a new_comment goes for a recipient, given who
// the comment is written to. Who wrote it does not matter: a person's comment
// and an agent's reply are routed the same way. ok is false when the
// recipient muted it.
func RouteComment(prefs map[string]string, audience CommentAudience) (section string, ok bool) {
	switch audience {
	case CommentToRecipient:
		return routeTo(prefs, commentToRecipientKind)
	case CommentToOthers:
		return routeTo(prefs, routeKindByKey[RouteKeyCommentsMentioningOthers])
	default:
		return routeTo(prefs, routeKindByKey[RouteKeyCommentsWithoutMentions])
	}
}

func routeTo(prefs map[string]string, kind routeKind) (section string, ok bool) {
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
