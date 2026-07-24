// Package issueevent defines the single strongly-typed contract for the
// issue:updated realtime event (MUL-4332 §5 domain-boundary work, review a′).
//
// Before this package the payload was a map[string]any whose issue value the
// in-memory listeners type-asserted to handler.IssueResponse. That coupling had
// two consequences the review flagged:
//
//   - Producers that emitted a plain map instead of a handler.IssueResponse
//     (the background task status reset) silently no-op'd every listener, so a
//     status change from that path recorded no activity and sent no inbox
//     notification. The distinction "authoritative change vs. realtime-only
//     reconcile" was encoded by accident as the runtime type of one map key.
//   - The batch update emitted a reduced payload with no prev_status, so its
//     activity-log entries read from: "".
//
// This package lives below internal/handler (it imports only db + util) so the
// executor in internal/service can build the same payload the handlers do; a
// service package cannot import handler without a cycle, which is why the HTTP
// response type could not be reused directly.
package issueevent

import (
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueSnapshot is the typed, handler-free projection of an issue that in-memory
// issue:updated listeners read, replacing the handler.IssueResponse assertion.
type IssueSnapshot struct {
	ID           string
	WorkspaceID  string
	Status       string
	Priority     string
	Title        string
	AssigneeType *string
	AssigneeID   *string
	Description  *string
	StartDate    *string
	DueDate      *string
	CreatorType  string
	CreatorID    string
}

// SnapshotOf projects a database issue into the listener-facing snapshot.
func SnapshotOf(i db.Issue) IssueSnapshot {
	return IssueSnapshot{
		ID:           util.UUIDToString(i.ID),
		WorkspaceID:  util.UUIDToString(i.WorkspaceID),
		Status:       i.Status,
		Priority:     i.Priority,
		Title:        i.Title,
		AssigneeType: util.TextToPtr(i.AssigneeType),
		AssigneeID:   util.UUIDToPtr(i.AssigneeID),
		Description:  util.TextToPtr(i.Description),
		StartDate:    util.DateToPtr(i.StartDate),
		DueDate:      util.DateToPtr(i.DueDate),
		CreatorType:  i.CreatorType,
		CreatorID:    util.UUIDToString(i.CreatorID),
	}
}

// IssueUpdatedPayload is the single payload every issue:updated producer emits and
// every listener consumes. Its JSON tags reproduce the exact wire the realtime
// client already receives (cmd/server/listeners.go marshals Event.Payload as-is);
// the json:"-" fields are internal and carry the typed view the in-memory
// listeners read.
type IssueUpdatedPayload struct {
	// Issue is the producer's own client representation of the issue, emitted on
	// the wire unchanged. It stays `any` so each producer keeps its existing shape
	// (the HTTP handlers pass a handler.IssueResponse, the task path a map).
	Issue any `json:"issue"`

	AssigneeChanged    bool `json:"assignee_changed"`
	StatusChanged      bool `json:"status_changed"`
	PriorityChanged    bool `json:"priority_changed"`
	ProjectChanged     bool `json:"project_changed"`
	StartDateChanged   bool `json:"start_date_changed"`
	DueDateChanged     bool `json:"due_date_changed"`
	DescriptionChanged bool `json:"description_changed"`
	TitleChanged       bool `json:"title_changed"`

	PrevTitle        string  `json:"prev_title"`
	PrevAssigneeType *string `json:"prev_assignee_type"`
	PrevAssigneeID   *string `json:"prev_assignee_id"`
	PrevStatus       string  `json:"prev_status"`
	PrevPriority     string  `json:"prev_priority"`
	PrevStartDate    *string `json:"prev_start_date"`
	PrevDueDate      *string `json:"prev_due_date"`
	PrevDescription  *string `json:"prev_description"`
	CreatorType      string  `json:"creator_type"`
	CreatorID        string  `json:"creator_id"`

	// Source labels the producer for the few clients that branch on it (the
	// GitHub-merge path sets it). omitempty keeps it off every other producer's
	// wire, so their payloads are unchanged.
	Source string `json:"source,omitempty"`

	// Snapshot is the typed issue the listeners read. Never serialized.
	Snapshot IssueSnapshot `json:"-"`

	// TriggerSideEffects separates an authoritative change that must record
	// activity, notify and drive autopilot/subscriber updates (a user or system
	// action) from an internal realtime-only reconcile (a background status
	// reset). This makes explicit the distinction that was previously encoded by
	// accident as "issue is a handler.IssueResponse" vs "issue is a map"; the
	// listeners gate on it instead of on a runtime type assertion. Never
	// serialized — it selects side effects, it is not client wire.
	TriggerSideEffects bool `json:"-"`
}

// Build computes the typed payload from the LOCKED before/after pre-image, the
// after-image, the client representation the producer already built, and whether
// this change should fire side effects. Every changed/prev field derives purely
// from before/after (review a′: "只以锁内 before/after 为准"), so the single,
// batch, GitHub-merge and task-reset producers all emit the same shape and can
// never drift apart. Creator is immutable, so it is read from the pre-image to
// match the historical single-update payload exactly.
func Build(before, after db.Issue, issue any, sideEffects bool) IssueUpdatedPayload {
	return IssueUpdatedPayload{
		Issue:              issue,
		StatusChanged:      before.Status != after.Status,
		PriorityChanged:    before.Priority != after.Priority,
		TitleChanged:       before.Title != after.Title,
		ProjectChanged:     util.UUIDToString(before.ProjectID) != util.UUIDToString(after.ProjectID),
		AssigneeChanged:    !ptrEqual(util.TextToPtr(before.AssigneeType), util.TextToPtr(after.AssigneeType)) || !ptrEqual(util.UUIDToPtr(before.AssigneeID), util.UUIDToPtr(after.AssigneeID)),
		DescriptionChanged: !ptrEqual(util.TextToPtr(before.Description), util.TextToPtr(after.Description)),
		StartDateChanged:   !ptrEqual(util.DateToPtr(before.StartDate), util.DateToPtr(after.StartDate)),
		DueDateChanged:     !ptrEqual(util.DateToPtr(before.DueDate), util.DateToPtr(after.DueDate)),

		PrevTitle:        before.Title,
		PrevAssigneeType: util.TextToPtr(before.AssigneeType),
		PrevAssigneeID:   util.UUIDToPtr(before.AssigneeID),
		PrevStatus:       before.Status,
		PrevPriority:     before.Priority,
		PrevStartDate:    util.DateToPtr(before.StartDate),
		PrevDueDate:      util.DateToPtr(before.DueDate),
		PrevDescription:  util.TextToPtr(before.Description),
		CreatorType:      before.CreatorType,
		CreatorID:        util.UUIDToString(before.CreatorID),

		Snapshot:           SnapshotOf(after),
		TriggerSideEffects: sideEffects,
	}
}

// ptrEqual compares two optional strings by value: both nil is equal, one nil is
// not, otherwise the pointed-to values must match.
func ptrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
