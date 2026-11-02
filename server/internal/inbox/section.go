package inbox

import "github.com/jackc/pgx/v5/pgtype"

// Every notification belongs to one of two Inbox sections. "action" is what
// waits on the recipient — an assignment, a mention, a failed run, a person
// talking on their issue — and is the only section that counts as unread.
// "activity" is routine progress the Work sidebar already tracks live (status
// moves, agent replies, property edits); it stays listable but never raises
// the unread badge.
//
// The section is derived on read from (type, actor_type) rather than stored,
// so rows written before the split classify exactly like new ones.
const (
	SectionAction   = "action"
	SectionActivity = "activity"
)

// ActivityTypes are always activity, whoever caused them. task_completed,
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

// AgentActivityTypes are activity when an agent caused them and action when a
// person did: an agent's reply is the run's result, which the sidebar already
// shows; a person commenting on your issue usually wants you.
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

// Section classifies a notification. Unknown types are action, so a new kind
// is never hidden from the unread count by default. The SQL predicates that
// count unread rows apply the same rule through ActivityTypes and
// AgentActivityTypes; keep the two in step.
func Section(notifType string, actorType pgtype.Text) string {
	if activityTypeSet[notifType] {
		return SectionActivity
	}
	if agentActivityTypeSet[notifType] && actorType.Valid && actorType.String == "agent" {
		return SectionActivity
	}
	return SectionAction
}
