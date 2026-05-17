// CEREBRO-PATCH(cerebro-listeners): this entire file is cerebro-only — it
// implements per-user notification routing (inbox / notifications-tab / off)
// driven by user.preferences. Upstream baseline has no equivalent. Kept in
// package main alongside notification_listeners.go because the listener
// helpers call resolveRoute / issueAssigneeMemberID / responseAssigneeMemberID
// directly; moving to a subpackage would force ~12 import-site edits in the
// (heavily inline-modified) upstream listeners file and increase merge
// conflict surface rather than reduce it. See docs/upstream-sync/01-audit.md
// row "server/cmd/server/notification_listeners.go" for the rationale.
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

// Channels are independent destinations a notification can land on. A single
// event can flow to multiple channels (e.g. inbox + mobile push) — they are
// not mutually exclusive. Mirror of `Channel` in
// packages/core/notifications/routing.ts.
const (
	channelInbox         = "inbox"
	channelNotifications = "notifications"
	channelMobile        = "mobile"
	channelDesktop       = "desktop"
	channelMail          = "mail"
)

// allChannels is the canonical iteration order for resolver / migration code.
var allChannels = []string{
	channelInbox,
	channelNotifications,
	channelMobile,
	channelDesktop,
	channelMail,
}

// defaultChannelChoices is the per-channel default when the user has no
// override. Mirrors the design in the channel-first mockup. Keys match
// defaultRoutes exactly; values say whether the channel fires for that key
// out-of-the-box.
var defaultChannelChoices = map[string]map[string]bool{
	channelInbox: {
		"issue_assigned":              true,
		"mentioned":                   true,
		"task_failed":                 true,
		"unassigned":                  true,
		"reaction_added":              false,
		"new_comment":                 false,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
	},
	channelNotifications: {
		"issue_assigned":              false,
		"mentioned":                   false,
		"task_failed":                 false,
		"unassigned":                  false,
		"reaction_added":              true,
		"new_comment":                 true,
		"assignee_changed":            true,
		"status_changed":              true,
		"start_date_changed.assignee": false,
		"start_date_changed.follower": true,
		"due_date_changed.assignee":   false,
		"due_date_changed.follower":   true,
		"priority_changed.assignee":   false,
		"priority_changed.follower":   true,
	},
	channelMobile: {
		"issue_assigned":              true,
		"mentioned":                   true,
		"task_failed":                 false,
		"unassigned":                  false,
		"reaction_added":              false,
		"new_comment":                 false,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
	},
	channelDesktop: {
		"issue_assigned":              true,
		"mentioned":                   true,
		"task_failed":                 true,
		"unassigned":                  false,
		"reaction_added":              false,
		"new_comment":                 false,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
	},
	// Mail is forward-compatible; no events fire by default until the
	// transport is built.
	channelMail: {},
}

// defaultChannelTransport mirrors the per-channel "Visning" defaults from the
// mockup — badge/sound/banner toggles that apply to the channel as a whole,
// independent of per-event routing.
type channelTransport struct {
	Badge  bool
	Sound  bool
	Banner bool
	Digest string
}

var defaultChannelTransport = map[string]channelTransport{
	channelMobile:  {Badge: true, Sound: true},
	channelDesktop: {Badge: true, Banner: true, Sound: false},
	channelMail:    {Digest: "daily"},
}

// resolveChannelChoice reports whether a channel should fire for this user
// and routing key. Reads
// `preferences.notifications.<channel>.<key>` ("on"/"off") and falls back to
// `defaultChannelChoices[channel][key]` when missing or invalid.
//
// Agents have no preferences; they receive everything on the inbox channel
// only (matches existing resolveRoute behavior for non-members).
//
// Master toggle: when `preferences.notifications.notify_all_mobile_inbox` is
// true, the mobile channel mirrors the inbox channel's resolution for the
// same key — every event that lands in inbox also fires a Web Push,
// regardless of per-key mobile prefs. Implements JEH-737. Other channels
// are unaffected.
func resolveChannelChoice(
	ctx context.Context,
	queries *db.Queries,
	recipientType, recipientID, channel, key string,
) bool {
	if recipientType != "member" {
		return channel == channelInbox
	}

	prefs, err := queries.GetUserPreferences(ctx, parseUUID(recipientID))
	if err != nil || len(prefs) == 0 {
		return channelDefault(channel, key)
	}

	var blob map[string]any
	if err := json.Unmarshal(prefs, &blob); err != nil {
		slog.Warn("preferences unmarshal failed", "user_id", recipientID, "error", err)
		return channelDefault(channel, key)
	}

	notifBlock, _ := blob["notifications"].(map[string]any)
	if notifBlock == nil {
		return channelDefault(channel, key)
	}

	// CEREBRO-PATCH(notify-all-mobile-inbox): JEH-737 master toggle — when set,
	// the mobile channel mirrors inbox routing for the same key, so every
	// inbox event also fires a Web Push without curating the per-key matrix.
	if channel == channelMobile {
		if notifyAll, _ := notifBlock["notify_all_mobile_inbox"].(bool); notifyAll {
			return resolveChannelChoiceFromBlock(notifBlock, channelInbox, key)
		}
	}

	return resolveChannelChoiceFromBlock(notifBlock, channel, key)
}

// resolveChannelChoiceFromBlock applies the same on/off lookup as
// resolveChannelChoice but starts from an already-unmarshalled
// notifications block, so we can re-resolve a different channel without
// re-fetching prefs from the DB. Used by the notify_all_mobile_inbox
// shortcut to mirror inbox resolution onto mobile.
func resolveChannelChoiceFromBlock(notifBlock map[string]any, channel, key string) bool {
	channelBlock, _ := notifBlock[channel].(map[string]any)
	if channelBlock == nil {
		return channelDefault(channel, key)
	}

	override, _ := channelBlock[key].(string)
	switch override {
	case "on":
		return true
	case "off":
		return false
	default:
		return channelDefault(channel, key)
	}
}

// resolveChannelTransport returns the channel-level transport settings
// (badge, sound, banner, digest) for a user. Reads
// `preferences.notifications.channels.<channel>` and overlays user-specified
// fields on top of the channel default.
func resolveChannelTransport(
	ctx context.Context,
	queries *db.Queries,
	recipientType, recipientID, channel string,
) channelTransport {
	t := defaultChannelTransport[channel]
	if recipientType != "member" {
		return t
	}

	prefs, err := queries.GetUserPreferences(ctx, parseUUID(recipientID))
	if err != nil || len(prefs) == 0 {
		return t
	}

	var blob map[string]any
	if err := json.Unmarshal(prefs, &blob); err != nil {
		return t
	}

	notifBlock, _ := blob["notifications"].(map[string]any)
	channelsBlock, _ := notifBlock["channels"].(map[string]any)
	override, _ := channelsBlock[channel].(map[string]any)
	if override == nil {
		return t
	}

	if v, ok := override["badge"].(bool); ok {
		t.Badge = v
	}
	if v, ok := override["sound"].(bool); ok {
		t.Sound = v
	}
	if v, ok := override["banner"].(bool); ok {
		t.Banner = v
	}
	if v, ok := override["digest"].(string); ok {
		t.Digest = v
	}
	return t
}

func channelDefault(channel, key string) bool {
	defaults, ok := defaultChannelChoices[channel]
	if !ok {
		return false
	}
	return defaults[key]
}

// splitTypes carry an .assignee / .follower suffix when looking up routing.
// The assignee gets a stronger signal than passive subscribers for these.
var splitTypes = map[string]bool{
	"due_date_changed":   true,
	"priority_changed":   true,
	"start_date_changed": true,
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
