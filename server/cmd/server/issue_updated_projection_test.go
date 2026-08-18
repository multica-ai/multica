package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Regression tests for the second half of MUL-5492: `issue:updated` used to
// broadcast prev_description alongside the new description, so every debounced
// description autosave pushed TWO full copies of the description to every
// connection in the workspace — including users who did not have the issue open.
//
// The projection has two halves and both must hold: the keys must not reach the
// wire, AND the in-process listeners that genuinely need them must still see
// them. A test that only checks the first half would pass just as happily if the
// producer had stopped populating the keys altogether, which would silently
// break mention notifications and the title-change activity.

// issueUpdatedPayload mirrors the map published by handler.UpdateIssue, trimmed
// to the fields these tests care about.
func issueUpdatedPayload() map[string]any {
	return map[string]any{
		"issue": map[string]any{
			"id":          "issue-1",
			"title":       "New title",
			"description": strings.Repeat("new body ", 1024),
		},
		"description_changed": true,
		"title_changed":       true,
		"prev_title":          "Old title",
		"prev_description":    strings.Repeat("old body ", 1024),
		"prev_status":         "todo",
	}
}

func TestIssueUpdatedBroadcast_OmitsFullPreviousDescription(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}

	// A type-specific listener stands in for the real in-process consumers
	// (subscriber_listeners, notification_listeners, activity_listeners). Publish
	// dispatches these before the SubscribeAll forwarder, so this also pins the
	// ordering the projection relies on.
	var seenByListener map[string]any
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		if p, ok := e.Payload.(map[string]any); ok {
			seenByListener = p
		}
	})

	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "ws-1",
		ActorID:     "member-1",
		ActorType:   "member",
		Payload:     issueUpdatedPayload(),
	})

	if len(fb.workspaceCalls) != 1 {
		t.Fatalf("BroadcastToWorkspace calls = %d, want 1", len(fb.workspaceCalls))
	}
	raw := fb.workspaceCalls[0].msg

	// Half 1: the internal-only keys must not be on the wire at all. Assert on
	// the raw bytes too — a nested copy would still cost the bandwidth this fix
	// is about, even if the top-level key were gone.
	if strings.Contains(string(raw), "prev_description") {
		t.Error("broadcast frame still contains prev_description")
	}
	if strings.Contains(string(raw), "prev_title") {
		t.Error("broadcast frame still contains prev_title")
	}

	var frame struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if frame.Type != protocol.EventIssueUpdated {
		t.Errorf("frame type = %q, want %q", frame.Type, protocol.EventIssueUpdated)
	}
	if _, present := frame.Payload["prev_description"]; present {
		t.Error("payload still carries prev_description")
	}
	if _, present := frame.Payload["prev_title"]; present {
		t.Error("payload still carries prev_title")
	}

	// Everything clients actually consume must survive untouched. The issue
	// object keeps its description: clients apply it to their cache, and
	// stripping it would just trade fanout bytes for N refetches.
	issue, ok := frame.Payload["issue"].(map[string]any)
	if !ok {
		t.Fatal("payload lost the issue object")
	}
	if issue["description"] == "" || issue["description"] == nil {
		t.Error("issue.description was stripped; clients need it for cache updates")
	}
	if frame.Payload["description_changed"] != true {
		t.Error("description_changed flag was lost")
	}
	if frame.Payload["title_changed"] != true {
		t.Error("title_changed flag was lost")
	}
	// Cheap scalar prev_* fields are deliberately kept.
	if frame.Payload["prev_status"] != "todo" {
		t.Error("prev_status should be preserved; only the large fields are internal-only")
	}

	// Half 2: the in-process listener still received the full payload.
	if seenByListener == nil {
		t.Fatal("in-process listener did not run")
	}
	if _, present := seenByListener["prev_description"]; !present {
		t.Error("in-process listener lost prev_description; mention diffing would break")
	}
	if _, present := seenByListener["prev_title"]; !present {
		t.Error("in-process listener lost prev_title; the title-change activity would break")
	}
}

// TestProjectOutbound_DoesNotMutateProducerPayload guards the copy-on-project
// behaviour. Mutating the producer's map in place would corrupt it for any
// listener or forwarder that reads it afterwards.
func TestProjectOutbound_DoesNotMutateProducerPayload(t *testing.T) {
	original := issueUpdatedPayload()

	projected := projectOutbound(protocol.EventIssueUpdated, original)

	if _, present := original["prev_description"]; !present {
		t.Error("projectOutbound mutated the producer's payload map")
	}
	pm, ok := projected.(map[string]any)
	if !ok {
		t.Fatalf("projected payload type = %T, want map[string]any", projected)
	}
	if _, present := pm["prev_description"]; present {
		t.Error("projected payload still has prev_description")
	}
	if len(pm) != len(original)-2 {
		t.Errorf("projected key count = %d, want %d (exactly two keys removed)", len(pm), len(original)-2)
	}
}

func TestProjectOutbound_TaskFailedKeepsErrorInternal(t *testing.T) {
	original := map[string]any{
		"task_id":        "task-1",
		"failure_reason": "timeout",
		"retry_pending":  false,
		"error":          "task timed out",
	}

	projected, ok := projectOutbound(protocol.EventTaskFailed, original).(map[string]any)
	if !ok {
		t.Fatal("task:failed projection did not return a map")
	}
	if _, present := projected["error"]; present {
		t.Fatal("task:failed error reached the workspace realtime payload")
	}
	if projected["failure_reason"] != "timeout" || projected["retry_pending"] != false {
		t.Fatalf("safe task failure metadata was lost: %#v", projected)
	}
	if original["error"] != "task timed out" {
		t.Fatal("task:failed projection mutated the in-process payload")
	}
}

func TestTaskFailedBroadcast_DeliversErrorOnlyInProcess(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	payload := map[string]any{
		"task_id":        "task-1",
		"failure_reason": "timeout",
		"retry_pending":  false,
		"error":          "task timed out",
	}

	var inProcessError string
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		m, _ := e.Payload.(map[string]any)
		inProcessError, _ = m["error"].(string)
	})
	registerListeners(bus, fb)
	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: "workspace-1",
		Payload:     payload,
	})

	if inProcessError != "task timed out" {
		t.Fatalf("in-process channel listener error = %q, want task timed out", inProcessError)
	}
	if len(fb.workspaceCalls) != 1 {
		t.Fatalf("workspace broadcasts = %d, want 1", len(fb.workspaceCalls))
	}
	var frame struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(fb.workspaceCalls[0].msg, &frame); err != nil {
		t.Fatalf("unmarshal workspace frame: %v", err)
	}
	if _, present := frame.Payload["error"]; present {
		t.Fatal("channel-only failure error reached the workspace broadcast")
	}
	if payload["error"] != "task timed out" {
		t.Fatal("broadcast projection mutated the producer payload")
	}
}

// TestProjectOutbound_PassesThroughUnlistedEvents keeps the projection from
// becoming a general-purpose payload filter: an event type with no entry in the
// table must be forwarded byte-for-byte, and a non-map payload must survive.
func TestProjectOutbound_PassesThroughUnlistedEvents(t *testing.T) {
	payload := map[string]any{"prev_description": "kept"}
	if got := projectOutbound(protocol.EventIssueCreated, payload); got == nil {
		t.Fatal("unlisted event type returned nil payload")
	} else if m, ok := got.(map[string]any); !ok || m["prev_description"] != "kept" {
		t.Error("unlisted event type must pass through untouched")
	}

	// Typed (non-map) payloads are common elsewhere in the bus.
	type typedPayload struct{ ID string }
	tp := typedPayload{ID: "x"}
	if got := projectOutbound(protocol.EventIssueUpdated, tp); got != any(tp) {
		t.Errorf("non-map payload was altered: got %#v, want %#v", got, tp)
	}
}
