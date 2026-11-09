package inbox

// TurnTypes are the notification types that put an issue in its recipient's
// Inbox: the issue was handed to them. An agent finished a run they started,
// someone @-mentioned them, the issue was assigned to them, an agent waits on
// them to confirm an app call or connect an account, or a result they asked
// for (quick create, an autopilot they follow, a finished stage, an app call)
// arrived.
//
// Every other type (status, comment, date and priority changes, reactions) is
// still recorded, but it does not ask anything of the recipient, so the Inbox
// neither lists nor announces it. The issue's own timeline shows that history.
var TurnTypes = []string{
	"issue_assigned",
	"mentioned",
	"agent_completed",
	"agent_blocked",
	"task_failed",
	"review_requested",
	"children_done",
	"issue_subscribed",
	"quick_create_done",
	"quick_create_failed",
	"quick_create_unconfirmed",
	"autopilot_paused",
	"autopilot_quota_exceeded",
	"app_action_requested",
	"app_action_reminder",
	"app_connect_requested",
	"app_action_result",
}

var turnTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(TurnTypes))
	for _, t := range TurnTypes {
		set[t] = true
	}
	return set
}()

// IsTurn reports whether a notification of this type belongs in the Inbox.
func IsTurn(notifType string) bool {
	return turnTypeSet[notifType]
}
