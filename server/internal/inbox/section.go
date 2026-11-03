package inbox

import "github.com/jackc/pgx/v5/pgtype"

// Every notification belongs to one of two Inbox sections. "action" is what
// waits on the recipient — an assignment, a mention, a failed run, a person
// talking on their issue — and is the only section that counts as unread.
// "activity" is routine progress the Work sidebar already tracks live (status
// moves, agent replies, property edits); it stays listable but never raises
// the unread badge.
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

// AgentActivityTypes are activity by default when an agent caused them and
// action when a person did: an agent's reply is the run's result, which the
// sidebar already shows; a person commenting on your issue usually wants you.
var AgentActivityTypes = []string{
	"new_comment",
}

var (
	activityTypeSet      = toSet(ActivityTypes)
	agentActivityTypeSet = toSet(AgentActivityTypes)
)

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// DefaultSection classifies a notification nobody routed. Unknown types are
// action, so a new kind is never hidden from the unread count by default. The
// SQL predicates apply the same rule to rows without a stored section through
// ActivityTypes and AgentActivityTypes; keep the two in step.
func DefaultSection(notifType string, actorType pgtype.Text) string {
	if activityTypeSet[notifType] {
		return SectionActivity
	}
	if agentActivityTypeSet[notifType] && actorType.Valid && actorType.String == "agent" {
		return SectionActivity
	}
	return SectionAction
}

// ItemSection is the section of a stored row: the one it was routed to, or the
// default for rows written before routing existed.
func ItemSection(stored pgtype.Text, notifType string, actorType pgtype.Text) string {
	if stored.Valid && (stored.String == SectionAction || stored.String == SectionActivity) {
		return stored.String
	}
	return DefaultSection(notifType, actorType)
}

// SectionText is the value written to inbox_item.section.
func SectionText(section string) pgtype.Text {
	return pgtype.Text{String: section, Valid: true}
}
