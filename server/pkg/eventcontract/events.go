// Package eventcontract names business facts independently of plugins or transport.
package eventcontract

const (
	EventIssueCreated       = "issue.created"
	EventIssueUpdated       = "issue.updated"
	EventIssueStatusChanged = "issue.status_changed"
	EventCommentCreated     = "comment.created"
	EventTaskStarted        = "task.started"
	EventTaskCompleted      = "task.completed"
	EventTaskFailed         = "task.failed"
	EventTaskCancelled      = "task.cancelled"
)

// WakeupTypes lists only facts with transactional capture, not every bus event.
var WakeupTypes = []string{EventTaskCompleted, EventTaskFailed, EventTaskCancelled, EventCommentCreated, EventIssueStatusChanged}
