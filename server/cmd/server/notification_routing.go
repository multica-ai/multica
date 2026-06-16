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
		"issue_assigned": true,
		"mentioned":      true,
		// CEREBRO-PATCH(inbox-reminder-push): reminder is a separate routing key for reminder-only push.
		"reminder":       true,
		"task_failed":    true,
		"unassigned":     true,
		"reaction_added": false,
		"new_comment":    true, // CEREBRO-PATCH(new-comment-inbox-default): TECH-3001 — default to Jesper's preference
		// CEREBRO-PATCH(dm-push): FIR-308 — direct messages get their own routing key so a person-to-person DM can be muted/kept independently of issue-comment traffic. Inbox on by default, mirroring new_comment; the row is folded into the DM channel row in the inbox.
		"dm_message":                  true,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
		// CEREBRO-PATCH(issue-date-reminders): fire a reminder to the assignee when the date arrives.
		"due_date_reminder":   true,
		"start_date_reminder": true,
		// CEREBRO-PATCH(agent-comment-tag-split): TECH-2961 — three routing keys
		// for agent-authored comments split by tag, so users can mute monologues
		// without losing real hand-offs. See cerebro_agent_comment_routing.go
		// for the splitter; defaults follow the rule "hand-offs stay visible,
		// chatter goes quiet."
		"agent_comment_no_tag":     false,
		"agent_comment_member_tag": true,
		"agent_comment_agent_tag":  true,
		// CEREBRO-PATCH(system-notification-routing): route platform-authored notifications through the channel matrix.
		"system_notification": true,
	},
	channelNotifications: {
		"issue_assigned": false,
		"mentioned":      false,
		"reminder":       false,
		"task_failed":    false,
		"unassigned":     false,
		"reaction_added": true,
		"new_comment":    true,
		// CEREBRO-PATCH(dm-push): FIR-308 — DMs also surface in the notifications feed by default (parity with new_comment).
		"dm_message":                  true,
		"assignee_changed":            true,
		"status_changed":              true,
		"start_date_changed.assignee": false,
		"start_date_changed.follower": true,
		"due_date_changed.assignee":   false,
		"due_date_changed.follower":   true,
		"priority_changed.assignee":   false,
		"priority_changed.follower":   true,
		// CEREBRO-PATCH(issue-date-reminders): date reminders are personal — they ring the inbox, not the notifications feed.
		"due_date_reminder":   false,
		"start_date_reminder": false,
		// CEREBRO-PATCH(agent-comment-tag-split): TECH-2961 — still surface agent chatter in the notifications feed by default; it just doesn't ring the inbox.
		"agent_comment_no_tag":     true,
		"agent_comment_member_tag": true,
		"agent_comment_agent_tag":  true,
		// CEREBRO-PATCH(system-notification-routing): route platform-authored notifications through the channel matrix.
		"system_notification": false,
	},
	channelMobile: {
		"issue_assigned": true,
		"mentioned":      true,
		"reminder":       false,
		"task_failed":    false,
		"unassigned":     false,
		"reaction_added": false,
		"new_comment":    true, // CEREBRO-PATCH(new-comment-inbox-default): TECH-3001 — default to Jesper's preference
		// CEREBRO-PATCH(dm-push): FIR-308 — push a DM to the phone by default. The message excerpt in the push body is gated separately by the per-user dm_excerpt preference.
		"dm_message":                  true,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
		// CEREBRO-PATCH(issue-date-reminders): push the reminder to mobile by default.
		"due_date_reminder":   true,
		"start_date_reminder": true,
		// CEREBRO-PATCH(agent-comment-tag-split): TECH-2961.
		"agent_comment_no_tag":     false,
		"agent_comment_member_tag": true,
		"agent_comment_agent_tag":  false,
		// CEREBRO-PATCH(system-notification-routing): route platform-authored notifications through the channel matrix.
		"system_notification": false,
	},
	channelDesktop: {
		"issue_assigned": true,
		"mentioned":      true,
		"reminder":       false,
		"task_failed":    true,
		"unassigned":     false,
		"reaction_added": false,
		"new_comment":    false,
		// CEREBRO-PATCH(dm-push): FIR-308 — push a DM to the computer browser (web push) by default, so "phone + browser" both fire out of the box.
		"dm_message":                  true,
		"assignee_changed":            false,
		"status_changed":              false,
		"start_date_changed.assignee": true,
		"start_date_changed.follower": false,
		"due_date_changed.assignee":   true,
		"due_date_changed.follower":   false,
		"priority_changed.assignee":   true,
		"priority_changed.follower":   false,
		// CEREBRO-PATCH(issue-date-reminders): show a desktop banner for the reminder by default.
		"due_date_reminder":   true,
		"start_date_reminder": true,
		// CEREBRO-PATCH(agent-comment-tag-split): TECH-2961.
		"agent_comment_no_tag":     false,
		"agent_comment_member_tag": true,
		"agent_comment_agent_tag":  false,
		// CEREBRO-PATCH(system-notification-routing): route platform-authored notifications through the channel matrix.
		"system_notification": false,
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
// true, the mobile channel mirrors the inbox channel's resolution only for
// keys whose default primary channel is inbox. General notification-primary
// events (for example new_comment) are not promoted to mobile by the master
// toggle. Implements JEH-737 / FIR-1839. Other channels are unaffected.
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
	// the mobile channel mirrors inbox routing only for inbox-primary keys, so
	// focused inbox events also fire a Web Push without promoting general
	// notification-primary chatter like new_comment.
	if channel == channelMobile {
		if notifyAll, _ := notifBlock["notify_all_mobile_inbox"].(bool); notifyAll {
			if primaryChannelForKey(key) != channelInbox {
				return false
			}
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

// CEREBRO-PATCH(notify-all-mobile-inbox-primary): keep the mobile inbox
// master toggle scoped to default inbox-primary routing keys only.
// primaryChannelForKey returns the default storage channel a routing key
// belongs to. The notify-all-mobile-inbox master toggle uses this to avoid
// promoting notification-primary events to mobile push just because the user
// manually routes that key into inbox.
func primaryChannelForKey(key string) string {
	if defaultChannelChoices[channelInbox][key] {
		return channelInbox
	}
	if defaultChannelChoices[channelNotifications][key] {
		return channelNotifications
	}
	return ""
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

// CEREBRO-PATCH(dm-push): FIR-308 — direct-message push helpers. A DM is a
// comment on a kind='dm' issue; it routes under the dedicated "dm_message" key
// (see the comment-created listener) and produces a recipient-localized push
// whose body is only included when the recipient opted into showing the
// message excerpt. Privacy default: name only, no message content.

// resolveDMExcerpt reports whether the recipient opted to show the message
// excerpt in DM push notifications. Reads
// `preferences.notifications.dm_excerpt` (bool). Default false — the push shows
// only the sender's name unless the user explicitly turns the excerpt on.
func resolveDMExcerpt(ctx context.Context, queries *db.Queries, recipientID string) bool {
	prefs, err := queries.GetUserPreferences(ctx, parseUUID(recipientID))
	if err != nil || len(prefs) == 0 {
		return false
	}
	var blob map[string]any
	if err := json.Unmarshal(prefs, &blob); err != nil {
		return false
	}
	notif, _ := blob["notifications"].(map[string]any)
	if notif == nil {
		return false
	}
	v, _ := notif["dm_excerpt"].(bool)
	return v
}

// resolveUserLanguage returns the user's UI language ("da" or "en"), defaulting
// to Danish — Firtal's primary language and the user_profile default — when the
// column is unset or carries an unrecognized value.
func resolveUserLanguage(ctx context.Context, queries *db.Queries, userID string) string {
	if u, err := queries.GetUser(ctx, parseUUID(userID)); err == nil && u.Language.Valid {
		if u.Language.String == "en" {
			return "en"
		}
	}
	return "da"
}

// dmSenderDisplayName resolves the human-readable name of whoever sent the DM,
// for use in the push title. Falls back to a generic word when the actor can't
// be resolved so the push still reads naturally.
func dmSenderDisplayName(ctx context.Context, queries *db.Queries, actorType, actorID, lang string) string {
	if actorID != "" {
		switch actorType {
		case "member":
			if u, err := queries.GetUser(ctx, parseUUID(actorID)); err == nil && u.Name != "" {
				return u.Name
			}
		case "agent":
			if a, err := queries.GetAgent(ctx, parseUUID(actorID)); err == nil && a.Name != "" {
				return a.Name
			}
		}
	}
	if lang == "en" {
		return "someone"
	}
	return "nogen"
}

// dmPushContent builds the (title, body) of a DM push for one recipient. The
// title is always "<New message from> <sender>"; the body carries the message
// excerpt only when the recipient enabled dm_excerpt, otherwise it is empty so
// the push reveals the sender but not the content.
func dmPushContent(ctx context.Context, queries *db.Queries, recipientID, actorType, actorID, content string) (string, string) {
	lang := resolveUserLanguage(ctx, queries, recipientID)
	sender := dmSenderDisplayName(ctx, queries, actorType, actorID, lang)
	title := "Ny besked fra " + sender
	if lang == "en" {
		title = "New message from " + sender
	}
	if resolveDMExcerpt(ctx, queries, recipientID) {
		return title, content
	}
	return title, ""
}
