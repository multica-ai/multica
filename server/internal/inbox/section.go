package inbox

import "github.com/jackc/pgx/v5/pgtype"

// Every notification belongs to one of two Inbox sections. "action" is what
// waits on the recipient — an assignment, a mention, a failed run, a comment
// written to them or to nobody in particular — and is the only section that
// counts as unread. "activity" is routine progress the Work sidebar already
// tracks live (status moves, property edits) and comments written to someone
// else; it stays listable but never raises the unread badge.
//
// A row written since recipients could route notifications (see Route)
// stores its section. Older rows store none and classify by DefaultSection,
// so the two kinds of row read the same way without a backfill.
const (
	SectionAction   = "action"
	SectionActivity = "activity"
)

// ActivityTypes are activity by default, whoever caused them. task_completed,
// agent_completed and issue_subscribed are no longer written but may exist in
// older rows.
//
// new_comment is not here: whether a comment is activity depends on who it
// @mentions (see RouteComment), which a row without a stored section does not
// record, so such rows stay action.
var ActivityTypes = []string{
	"status_changed",
	"priority_changed",
	"start_date_changed",
	"due_date_changed",
	"assignee_changed",
	"unassigned",
	"reaction_added",
	"quick_create_done",
	"task_completed",
	"agent_completed",
	"issue_subscribed",
}

var activityTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(ActivityTypes))
	for _, v := range ActivityTypes {
		set[v] = true
	}
	return set
}()

// DefaultSection classifies a notification nobody routed. Unknown types are
// action, so a new kind is never hidden from the unread count by default. The
// SQL predicates apply the same rule to rows without a stored section through
// ActivityTypes; keep the two in step.
func DefaultSection(notifType string) string {
	if activityTypeSet[notifType] {
		return SectionActivity
	}
	return SectionAction
}

// ItemSection is the section of a stored row: the one it was routed to, or the
// default for rows written before routing existed.
func ItemSection(stored pgtype.Text, notifType string) string {
	if stored.Valid && (stored.String == SectionAction || stored.String == SectionActivity) {
		return stored.String
	}
	return DefaultSection(notifType)
}

// SectionText is the value written to inbox_item.section.
func SectionText(section string) pgtype.Text {
	return pgtype.Text{String: section, Valid: true}
}
