package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Listener subscribes the workflow engine to the in-process event bus.
//
// Phase 1 wired only issue:updated (status transitions). Phase 3 adds
// comment:created (TriggerCommentMention) and issue:created (sub-issue
// case → TriggerSubIssueCreated), plus an additional code path inside
// onIssueUpdated for TriggerAllChildrenDone when the closed issue is a
// child of some parent.
type Listener struct {
	svc *Service
}

func NewListener(svc *Service) *Listener {
	return &Listener{svc: svc}
}

// Attach registers the listener on the supplied bus. Safe to call once at
// server startup. The engine itself decides per-event whether to act —
// CEREBRO_WORKFLOWS_ENABLED gating happens inside Service.Execute so the
// bus subscription remains in place across config flips.
func (l *Listener) Attach(bus *events.Bus) {
	bus.Subscribe(protocol.EventIssueUpdated, l.onIssueUpdated)
	bus.Subscribe(protocol.EventCommentCreated, l.onCommentCreated)
	bus.Subscribe(protocol.EventIssueCreated, l.onIssueCreated)
}

func (l *Listener) onIssueUpdated(e events.Event) {
	if !l.svc.Enabled() {
		return
	}

	payload, ok := payloadToMap(e.Payload)
	if !ok {
		return
	}

	// Phase 1 only cares about status transitions on this event. Title /
	// priority / assignee transitions are documented future triggers but
	// don't ship here.
	statusChanged, _ := payload["status_changed"].(bool)
	if !statusChanged {
		return
	}

	issue, ok := payload["issue"].(map[string]any)
	if !ok {
		return
	}

	prevStatus, _ := payload["prev_status"].(string)
	toStatus, _ := issue["status"].(string)
	issueID, _ := issue["id"].(string)
	projectID, _ := issue["project_id"].(string)
	parentIssueID, _ := issue["parent_issue_id"].(string)

	te := TriggerEvent{
		EventID:       deriveEventID(e, issueID, toStatus),
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     projectID,
		IssueID:       issueID,
		ParentIssueID: parentIssueID,
		Type:          TriggerStatusChanged,
		FromStatus:    prevStatus,
		ToStatus:      toStatus,
		Raw:           payload,
	}

	if err := l.svc.Dispatch(context.Background(), te); err != nil {
		slog.Error("workflow dispatch failed",
			"event_type", e.Type,
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
	}

	// Phase-3: also evaluate the all_children_done post-condition when the
	// closed issue is a child. Done after the status_changed dispatch so the
	// two paths can't bleed into each other.
	if toStatus == "done" && parentIssueID != "" {
		l.maybeFireAllChildrenDone(e, payload, parentIssueID)
	}
}

// maybeFireAllChildrenDone runs the sibling-count probe and dispatches a
// TriggerAllChildrenDone event when the parent has zero remaining open
// children. The probe takes a FOR UPDATE lock on the matching rows so two
// concurrent "last child → done" transitions serialize, preventing the
// trigger from firing twice for the same all-done state.
func (l *Listener) maybeFireAllChildrenDone(e events.Event, payload map[string]any, parentIssueID string) {
	parentID, ok := optionalUUID(parentIssueID)
	if !ok {
		return
	}
	wsID, ok := optionalUUID(e.WorkspaceID)
	if !ok {
		return
	}

	row, err := l.svc.queries.CountNonDoneChildrenLocked(context.Background(), cerebrodb.CountNonDoneChildrenLockedParams{
		ParentIssueID: parentID,
		WorkspaceID:   wsID,
	})
	if err != nil {
		slog.Warn("workflow all_children_done: count failed",
			"parent_issue_id", parentIssueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
		return
	}
	if row.OpenCount > 0 {
		return
	}
	// EventID is deterministic per all-done state. Re-opening a child and
	// re-closing it produces a new last_done_at (MAX(updated_at) over
	// done/cancelled siblings) and therefore a fresh EventID, so the
	// trigger fires again. Idempotency on the same all-done state
	// collapses retries on the workflow_idempotency_key table.
	lastDoneSuffix := ""
	if row.LastDoneAt.Valid {
		lastDoneSuffix = row.LastDoneAt.Time.UTC().Format(rfcNano)
	}

	issue, _ := payload["issue"].(map[string]any)
	projectID, _ := issue["project_id"].(string)

	te := TriggerEvent{
		EventID:       "all_children_done:" + parentIssueID + ":" + lastDoneSuffix,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     projectID,
		IssueID:       parentIssueID, // actions target the parent
		ParentIssueID: parentIssueID,
		Type:          TriggerAllChildrenDone,
		Raw:           payload,
	}
	if err := l.svc.Dispatch(context.Background(), te); err != nil {
		slog.Error("workflow all_children_done dispatch failed",
			"parent_issue_id", parentIssueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
	}
}

// onCommentCreated builds a TriggerCommentMention event from the
// EventCommentCreated payload. The handler decodes comment.content,
// comment.issue_id (CommentResponse uses issue_id, not nested issue), and
// the workspace ID from the bus event. Two listener calls for the same
// comment collapse on the comment-derived EventID.
func (l *Listener) onCommentCreated(e events.Event) {
	if !l.svc.Enabled() {
		return
	}
	payload, ok := payloadToMap(e.Payload)
	if !ok {
		return
	}
	comment, ok := payload["comment"].(map[string]any)
	if !ok {
		return
	}
	commentID, _ := comment["id"].(string)
	issueID, _ := comment["issue_id"].(string)
	if commentID == "" || issueID == "" {
		return
	}

	te := TriggerEvent{
		EventID:     commentMentionEventID(commentID),
		WorkspaceID: e.WorkspaceID,
		IssueID:     issueID,
		CommentID:   commentID,
		Type:        TriggerCommentMention,
		Raw:         payload,
	}
	if err := l.svc.Dispatch(context.Background(), te); err != nil {
		slog.Error("workflow comment_mention dispatch failed",
			"comment_id", commentID,
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
	}
}

// onIssueCreated builds a TriggerSubIssueCreated event when the new issue is
// a child of some other issue. Top-level issues (parent_issue_id empty) are
// ignored — they are not "sub-issues".
func (l *Listener) onIssueCreated(e events.Event) {
	if !l.svc.Enabled() {
		return
	}
	payload, ok := payloadToMap(e.Payload)
	if !ok {
		return
	}
	issue, ok := payload["issue"].(map[string]any)
	if !ok {
		return
	}
	parentIssueID, _ := issue["parent_issue_id"].(string)
	if parentIssueID == "" {
		return
	}
	issueID, _ := issue["id"].(string)
	if issueID == "" {
		return
	}
	projectID, _ := issue["project_id"].(string)

	te := TriggerEvent{
		EventID:       "sub_issue_created:" + issueID,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     projectID,
		IssueID:       issueID,
		ParentIssueID: parentIssueID,
		Type:          TriggerSubIssueCreated,
		Raw:           payload,
	}
	if err := l.svc.Dispatch(context.Background(), te); err != nil {
		slog.Error("workflow sub_issue_created dispatch failed",
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
	}
}

// payloadToMap normalises an event payload to a fully-flattened map[string]any.
// The in-process bus delivers Go values without a JSON round-trip, so nested
// fields may be typed structs (e.g. IssueResponse, CommentResponse) rather than
// map[string]any. Marshalling through JSON converts every nested struct so the
// type assertions in the listener callbacks work regardless of payload origin.
func payloadToMap(v any) (map[string]any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return m, true
}

const rfcNano = "2006-01-02T15:04:05.999999999Z07:00"

// commentMentionEventID is the EventID used to collapse retries of the same
// comment. Mirrors the spec: hash(comment.id || workflow.id). The workflow
// part is supplied by the idempotency key composition in
// Service.Execute, so the EventID itself only needs to identify the comment.
func commentMentionEventID(commentID string) string {
	return "comment_mention:" + commentID
}

// deriveEventID mints a fresh, stable id for one bus emission. The in-process
// bus is synchronous so a single emission only ever produces one logical
// event — the random tail is what lets two genuine transitions to the same
// status survive deduplication, while still giving the retry path a stable
// id (stored in cerebro_workflow_run.trigger_event) to hash against.
//
// When upstream eventually attaches its own id (e.g. once a Redis-backed
// replay layer lands), this helper becomes a one-liner that surfaces it.
func deriveEventID(_ events.Event, issueID, toStatus string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return issueID + ":" + toStatus + ":" + hex.EncodeToString(nonce[:])
}
