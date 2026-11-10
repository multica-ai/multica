package inbox

// TurnTypes are the notification types that put an issue in its recipient's
// Inbox: someone wrote or reacted to them, an agent finished a run they
// started, someone @-mentioned them, the issue was assigned to or taken from
// them, an agent waits on them to confirm an app call or connect an account,
// or a result they asked for (quick create, an autopilot they follow, a
// finished stage, an app call) arrived.
//
// Field changes on an issue they follow (status, priority, start and due
// dates, someone else's assignee) are still recorded, but the Inbox neither
// lists nor announces them. The issue's own timeline shows that history.
var TurnTypes = []string{
	"new_comment",
	"reaction_added",
	"issue_assigned",
	"unassigned",
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

// NoticeTypes are the Inbox notifications that only tell the recipient
// something and ask nothing of them. Opening the issue is all there is to do
// with one, so reading it archives it; every other type waits for the
// recipient to archive it or for the issue to move on.
var NoticeTypes = []string{
	"reaction_added",
	"unassigned",
	"issue_subscribed",
	"quick_create_done",
	"app_action_result",
}

var noticeTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(NoticeTypes))
	for _, t := range NoticeTypes {
		set[t] = true
	}
	return set
}()

// IsNotice reports whether a notification of this type is a notice.
func IsNotice(notifType string) bool {
	return noticeTypeSet[notifType]
}
