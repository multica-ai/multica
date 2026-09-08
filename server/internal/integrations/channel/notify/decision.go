// Package notify forwards a narrow whitelist of inbox notifications to the
// recipient's bound IM channel as a direct message, and records the ones a
// reply can be attributed back to.
//
// This package decides; it never sends. Sending belongs to the per-platform
// DMDeliverer adapters, because the generic channel.Channel.Send seam does
// not exist for every platform — wecomChannel.Send deliberately returns
// ErrSendNotSupported rather than keep a heuristic second outbound path
// alive (wecom/wecom_channel.go:566).
package notify

// Decision is the whitelist's verdict for one inbox notification.
type Decision struct {
	// Push reports whether this notification is worth interrupting a human
	// in their IM client.
	Push bool
	// Replyable reports whether a reply to the pushed message can be
	// attributed back to an issue. False when the notification has no issue
	// to inject a comment into.
	Replyable bool
}

// pushableStatusChange lists the effective status categories whose
// status_changed notification is worth a DM.
//
// in_review asks for a decision on completed work. blocked asks for the input
// or intervention needed to resume work. Both require a human now; routine
// progress and terminal outcomes do not. Same product judgement as
// delegatedStatusNotify (cmd/server/notification_listeners.go:89-104), scoped
// tighter because a DM is more interruptive than an inbox row.
//
// Changing this map is a code change on purpose. Whether a class of event
// deserves to interrupt anyone is a global product judgement; whether a given
// person wants to be interrupted is already answered by notification_preference,
// which gates before the inbox row exists.
var pushableStatusChange = map[string]bool{
	"in_review": true,
	"blocked":   true,
}

// pushableTypes lists non-status notification types that always push.
// The bool is Replyable: an entry is false when the notification carries no
// issue, so a reply would have nowhere to land.
var pushableTypes = map[string]bool{
	// task_failed is where the daemon's blocked classifications surface,
	// including taskfailure.ReasonAgentBlocked ("Waiting on human input").
	"task_failed": true,

	// Quick-create outcomes are about an issue that was never created, so
	// there is no issue_id and no comment target.
	"quick_create_failed":      false,
	"quick_create_unconfirmed": false,

	// Workspace idle notifications aggregate a whole workflow and therefore
	// have no single issue where a reply could be injected.
	"workspace_idle": false,
}

// Decide applies the push whitelist.
//
// effectiveStatus must already be normalised through issuestatus.Effective so
// a workspace's custom status is judged by the category it inherits, not by
// its literal key — the same reasoning deliverToSubscriber's allowlist uses.
// It is ignored for types other than status_changed.
func Decide(notifType, effectiveStatus string) Decision {
	if notifType == "status_changed" {
		if pushableStatusChange[effectiveStatus] {
			return Decision{Push: true, Replyable: true}
		}
		return Decision{}
	}
	replyable, ok := pushableTypes[notifType]
	if !ok {
		return Decision{}
	}
	return Decision{Push: true, Replyable: replyable}
}
