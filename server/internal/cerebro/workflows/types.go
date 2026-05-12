package workflows

// Trigger and action types are stored as TEXT in the DB so we can extend the
// catalog without migrations. The check constraints in 9020_cerebro_workflows
// pin the currently-known values.
const (
	TriggerStatusChanged   = "status_changed"
	TriggerDueDateReached  = "due_date_reached"
	TriggerDueTimeReached  = "due_time_reached"

	ActionSetStatus     = "set_status"
	ActionCreateSubIssue = "create_sub_issue"
	ActionSendReminder  = "send_reminder"
)

// TriggerConfigStatusChanged is the payload stored in cerebro_workflow.trigger_config
// for the TriggerStatusChanged trigger. Either field may be empty:
//
//	{ "from_status": "in_progress", "to_status": "in_review" }
//	{ "to_status": "done" }              -> any → done
//	{ }                                  -> any → any (rarely useful)
type TriggerConfigStatusChanged struct {
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// ActionConfigSetStatus — change the triggered issue's status.
type ActionConfigSetStatus struct {
	Status string `json:"status"`
}

// ActionConfigCreateSubIssue — create a sub-issue under the triggered issue.
// Title and Description may contain {{issue.field}}-style placeholders;
// rendering is deferred to PR 2 with the form-builder. Phase 1 substitutes
// only the literal {{title}} token to keep the action useful.
type ActionConfigCreateSubIssue struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssigneeID  string `json:"assignee_id,omitempty"`
	AssigneeType string `json:"assignee_type,omitempty"`
}

// ActionConfigSendReminder — write an inbox row + push notification.
// Lands in PR 2 alongside the inbox-integration wiring. Phase 1 accepts the
// config shape but the runtime returns ErrUnimplementedAction.
type ActionConfigSendReminder struct {
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Message       string `json:"message"`
}
