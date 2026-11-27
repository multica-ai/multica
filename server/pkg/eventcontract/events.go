// Package eventcontract names business facts independently of plugins or transport.
package eventcontract

const (
	EventIssueCreated              = "issue.created"
	EventIssueDeleted              = "issue.deleted"
	EventIssueUpdated              = "issue.updated"
	EventIssueStatusChanged        = "issue.status_changed"
	EventIssueAssigneeChanged      = "issue.assignee_changed"
	EventIssueParentChanged        = "issue.parent_changed"
	EventIssueProjectChanged       = "issue.project_changed"
	EventIssueLabelsChanged        = "issue.labels_changed"
	EventIssuePropertiesChanged    = "issue.properties_changed"
	EventIssueMetadataChanged      = "issue.metadata_changed"
	EventCommentCreated            = "comment.created"
	EventCommentUpdated            = "comment.updated"
	EventCommentDeleted            = "comment.deleted"
	EventCommentResolved           = "comment.resolved"
	EventCommentUnresolved         = "comment.unresolved"
	EventTaskQueued                = "task.queued"
	EventTaskDispatched            = "task.dispatched"
	EventTaskDeferred              = "task.deferred"
	EventTaskWaitingLocalDirectory = "task.waiting_local_directory"
	EventTaskStarted               = "task.started"
	EventTaskCompleted             = "task.completed"
	EventTaskFailed                = "task.failed"
	EventTaskCancelled             = "task.cancelled"
	EventReactionAdded             = "reaction.added"
	EventReactionRemoved           = "reaction.removed"
	EventAttachmentAttached        = "attachment.attached"
	EventAttachmentDetached        = "attachment.detached"
)

// WakeupTypes lists only facts with transactional capture, not every bus event.
var WakeupTypes = []string{
	EventTaskQueued, EventTaskDispatched, EventTaskStarted, EventTaskDeferred,
	EventTaskWaitingLocalDirectory, EventTaskCompleted, EventTaskFailed, EventTaskCancelled,
	EventIssueUpdated, EventIssueStatusChanged, EventIssueAssigneeChanged, EventIssueParentChanged,
	EventIssueProjectChanged, EventIssueLabelsChanged, EventIssuePropertiesChanged, EventIssueMetadataChanged,
	EventCommentCreated, EventCommentUpdated, EventCommentDeleted, EventCommentResolved, EventCommentUnresolved,
	EventReactionAdded, EventReactionRemoved, EventAttachmentAttached, EventAttachmentDetached,
}

// Lifecycle events exist on the platform but cannot wake their own issue:
// subscription requires an existing issue, and deletion withdraws its work.
var LifecycleTypes = []string{EventIssueCreated, EventIssueDeleted}

// TaskEvent translates persisted states to the public vocabulary.
func TaskEvent(status string) string {
	switch status {
	case "running":
		return EventTaskStarted
	case "queued", "dispatched", "deferred", "waiting_local_directory", "completed", "failed", "cancelled":
		return "task." + status
	default:
		return ""
	}
}
