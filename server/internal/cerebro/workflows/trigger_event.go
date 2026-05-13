package workflows

// TriggerEvent is the canonical, engine-internal projection of a domain
// event after the listener has decoded the bus payload. Each trigger type
// builds one of these and hands it to Service.Execute.
//
// The shape is intentionally flat (vs nested maps) so condition evaluation
// and idempotency hashing stay readable. The raw map is kept for cases where
// a condition path references a field we did not promote to a top-level
// attribute.
type TriggerEvent struct {
	// EventID uniquely identifies this bus event so retries of the SAME
	// event collapse on the idempotency key. Callers (the listener) generate
	// this if the upstream payload lacks one.
	EventID string

	// WorkspaceID, ProjectID, IssueID — common scope fields. ProjectID is
	// empty for issues that aren't bound to a project.
	WorkspaceID string
	ProjectID   string
	IssueID     string

	// Type matches a TriggerKind constant ("status_changed", etc).
	Type string

	// Status transition (only set for TriggerStatusChanged).
	FromStatus string
	ToStatus   string

	// Phase-3 helpers — populated when relevant, empty otherwise.
	//
	// CommentID identifies the comment row for TriggerCommentMention. It is
	// also folded into the idempotency EventID so two listener calls for the
	// same comment collapse on retry.
	//
	// ParentIssueID is the parent of the triggered issue for the
	// TriggerSubIssueCreated and TriggerAllChildrenDone paths — convenient
	// promoted access without a Raw["issue"]["parent_issue_id"] dive.
	CommentID     string
	ParentIssueID string

	// Raw is the un-projected payload, used by condition lookup paths that
	// dereference into nested issue fields (priority, labels, etc).
	Raw map[string]any
}
