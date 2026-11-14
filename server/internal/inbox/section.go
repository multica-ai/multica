package inbox

import "github.com/jackc/pgx/v5/pgtype"

// The Inbox lists issues by whose turn it is (MUL-9215). A notification is
// written into one of two sections:
//
//   - SectionNeedsYou: it is addressed to the recipient, so the issue waits on
//     them until they act on it or archive it. These are the rows the Inbox
//     badge counts, read or not.
//   - SectionUpdates: the recipient only follows the issue. Nothing waits on
//     them, the rows are not counted, and reading is all there is to do.
//
// The section is decided when the row is written, from who the recipient is to
// the issue, and stored in inbox_item.section. It is deliberately not decided
// by notification type alone: an agent's reply on an issue the recipient owns
// and the same reply on an issue they merely follow are the same type.
const (
	SectionNeedsYou = "needs_you"
	SectionUpdates  = "updates"
)

// NeedsYouTypes are the types only ever written to the person they are for: a
// mention, an assignment, a run handed back, an app call waiting on an answer,
// a paused autopilot. They need no relation check.
//
// The list also classifies rows written before sections were stored (see
// EffectiveSection) and is passed to the SQL that counts them, so both read
// one definition.
var NeedsYouTypes = []string{
	"mentioned",
	"issue_assigned",
	"review_requested",
	"agent_completed",
	"agent_blocked",
	"task_failed",
	"task_cancelled",
	"children_done",
	"app_action_requested",
	"app_action_reminder",
	"app_connect_requested",
	"autopilot_paused",
	"autopilot_quota_exceeded",
	"quick_create_failed",
	"quick_create_unconfirmed",
}

var needsYouTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(NeedsYouTypes))
	for _, t := range NeedsYouTypes {
		set[t] = true
	}
	return set
}()

// DefaultSection is the section of a row written straight to its recipient.
func DefaultSection(notifType string) string {
	if needsYouTypeSet[notifType] {
		return SectionNeedsYou
	}
	return SectionUpdates
}

// SubscriberSection is the section of a row fanned out to an issue's
// subscribers, where the same event reaches people with different stakes.
//
// addressed reports whether the recipient answers for the issue: they created
// it, it is assigned to them, an agent filed it on their behalf, or they
// started the run the event is about. waitsOnHuman reports whether the issue's
// status hands it to a person (in review, blocked, a custom started status).
//
// A comment, a failed run or a blocked agent needs the people who answer for
// the issue and is an update for everyone else. A status change needs them only
// when the new status waits on a person. Field edits are updates for everyone.
func SubscriberSection(notifType string, addressed, waitsOnHuman bool) string {
	switch notifType {
	case "new_comment", "task_failed", "agent_blocked":
		if addressed {
			return SectionNeedsYou
		}
	case "status_changed":
		if addressed && waitsOnHuman {
			return SectionNeedsYou
		}
	}
	return SectionUpdates
}

// EffectiveSection is the section a stored row belongs to.
//
// Rows written before sections were stored have none. Reading was handling
// then, so such a row needs the recipient only while it is unread and of a type
// addressed to them; everything else they already saw is an update. The SQL
// that counts the badge (CountNeedsYouInboxByWorkspace) applies the same rule.
func EffectiveSection(stored pgtype.Text, notifType string, read bool) string {
	if stored.Valid {
		if stored.String == SectionNeedsYou {
			return SectionNeedsYou
		}
		if stored.String == SectionUpdates {
			return SectionUpdates
		}
	}
	if !read && needsYouTypeSet[notifType] {
		return SectionNeedsYou
	}
	return SectionUpdates
}
