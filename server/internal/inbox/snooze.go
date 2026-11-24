package inbox

import (
	"encoding/json"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MaxSnooze is how far ahead an issue can be snoozed.
const MaxSnooze = 366 * 24 * time.Hour

// Values of details.mention_via on a "mentioned" row: how the mention reached
// the recipient.
const (
	MentionViaMember = "member"
	// MentionViaSquad is no longer written: an @squad mention only triggers
	// the squad leader and does not notify squad members. It is kept because
	// existing inbox rows still carry this value.
	MentionViaSquad = "squad"
	MentionViaAll   = "all"
)

// wakingTypes are the notifications that end a snooze early: each one asks
// something of the recipient, where the rest only report activity.
var wakingTypes = map[string]bool{
	"mentioned":            true,
	"issue_assigned":       true,
	"task_failed":          true,
	"app_action_requested": true,
	"app_action_reminder":  true,
	"children_done":        true,
	"blockers_done":        true,
}

// WakesSnooze reports whether a row of this type ends a snooze on its issue.
// A mention wakes only when it names the recipient; one that reached them
// through @all or a squad is counted like any other update.
func WakesSnooze(notifType string, details []byte) bool {
	if !wakingTypes[notifType] {
		return false
	}
	if notifType != "mentioned" || len(details) == 0 {
		return true
	}
	var d struct {
		MentionVia string `json:"mention_via"`
	}
	if err := json.Unmarshal(details, &d); err != nil {
		return true
	}
	return d.MentionVia != MentionViaAll && d.MentionVia != MentionViaSquad
}

// IsSnoozed reports whether the row is out of the inbox at now.
func IsSnoozed(item db.InboxItem, now time.Time) bool {
	return item.SnoozedUntil.Valid && item.SnoozedUntil.Time.After(now)
}

// NewItemEventType names the event that announces a row Deliver wrote. A row that joined a snooze is announced as inbox:snoozed, so no
// client raises a banner and no channel pushes it.
func NewItemEventType(item db.InboxItem) string {
	if IsSnoozed(item, time.Now()) {
		return protocol.EventInboxSnoozed
	}
	return protocol.EventInboxNew
}
