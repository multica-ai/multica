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

	// Raw is the un-projected payload, used by condition lookup paths that
	// dereference into nested issue fields (priority, labels, etc).
	Raw map[string]any
}
