package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Listener subscribes the workflow engine to the in-process event bus.
//
// PR 1 wires only the issue:updated event (which carries status_changed +
// prev_status in its payload, so we don't need a CEREBRO-PATCH adding a
// dedicated status_changed event). The due-date/due-time triggers fire
// from the cron sweeper, not the bus, so they don't register here.
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
}

func (l *Listener) onIssueUpdated(e events.Event) {
	if !l.svc.Enabled() {
		return
	}

	payload, ok := e.Payload.(map[string]any)
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

	te := TriggerEvent{
		EventID:     deriveEventID(e, issueID, toStatus),
		WorkspaceID: e.WorkspaceID,
		ProjectID:   projectID,
		IssueID:     issueID,
		Type:        TriggerStatusChanged,
		FromStatus:  prevStatus,
		ToStatus:    toStatus,
		Raw:         payload,
	}

	if err := l.svc.Dispatch(context.Background(), te); err != nil {
		slog.Error("workflow dispatch failed",
			"event_type", e.Type,
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
	}
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
