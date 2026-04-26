package realtime

import (
	"context"
	"testing"
)

func TestMirroredRelayPublishesSameEventIDToBothBackends(t *testing.T) {
	primary := &recordingManagedRelay{nodeID: "primary"}
	mirror := &recordingManagedRelay{nodeID: "mirror"}
	relay := NewMirroredRelay(primary, mirror)

	relay.PublishWithID(ScopeWorkspace, "workspace-1", "", []byte(`{"type":"issue:updated"}`), "event-1")

	if len(primary.calls) != 1 {
		t.Fatalf("expected primary publish call, got %d", len(primary.calls))
	}
	if len(mirror.calls) != 1 {
		t.Fatalf("expected mirror publish call, got %d", len(mirror.calls))
	}
	if primary.calls[0].eventID != "event-1" || mirror.calls[0].eventID != "event-1" {
		t.Fatalf("expected same event id, got primary=%q mirror=%q", primary.calls[0].eventID, mirror.calls[0].eventID)
	}
}

type relayPublishCall struct {
	scopeType string
	scopeID   string
	exclude   string
	frame     string
	eventID   string
}

type recordingManagedRelay struct {
	nodeID string
	calls  []relayPublishCall
}

func (r *recordingManagedRelay) NodeID() string                      { return r.nodeID }
func (r *recordingManagedRelay) Start(context.Context)               {}
func (r *recordingManagedRelay) Stop()                               {}
func (r *recordingManagedRelay) Wait()                               {}
func (r *recordingManagedRelay) BroadcastToWorkspace(string, []byte) {}
func (r *recordingManagedRelay) Broadcast([]byte)                    {}

func (r *recordingManagedRelay) BroadcastToScope(scopeType, scopeID string, frame []byte) {
	r.PublishWithID(scopeType, scopeID, "", frame, "")
}

func (r *recordingManagedRelay) SendToUser(userID string, frame []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	r.PublishWithID(ScopeUser, userID, exclude, frame, "")
}

func (r *recordingManagedRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) {
	r.calls = append(r.calls, relayPublishCall{
		scopeType: scopeType,
		scopeID:   scopeID,
		exclude:   exclude,
		frame:     string(frame),
		eventID:   id,
	})
}
