package realtime

import (
	"context"
	"errors"
	"testing"
)

func TestMirroredRelayPublishesSameEventIDToBothBackends(t *testing.T) {
	primary := &recordingManagedRelay{nodeID: "primary"}
	mirror := &recordingManagedRelay{nodeID: "mirror"}
	relay := NewMirroredRelay(primary, mirror)

	if err := relay.PublishWithID(ScopeWorkspace, "workspace-1", "", []byte(`{"type":"issue:updated"}`), "event-1"); err != nil {
		t.Fatalf("PublishWithID: %v", err)
	}

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

func TestMirroredRelayRecordsDivergenceWhenOneBackendFails(t *testing.T) {
	M.Reset()
	t.Cleanup(M.Reset)

	primary := &recordingManagedRelay{nodeID: "primary"}
	mirror := &recordingManagedRelay{nodeID: "mirror", publishErr: errors.New("mirror unavailable")}
	relay := NewMirroredRelay(primary, mirror)

	err := relay.PublishWithID(ScopeWorkspace, "workspace-1", "", []byte(`{"type":"issue:updated"}`), "event-1")

	if err == nil {
		t.Fatal("expected mirrored publish to return backend error")
	}
	if got := M.RedisMirrorPrimaryErrors.Load(); got != 0 {
		t.Fatalf("expected 0 primary errors, got %d", got)
	}
	if got := M.RedisMirrorSecondaryErrors.Load(); got != 1 {
		t.Fatalf("expected 1 secondary error, got %d", got)
	}
	if got := M.RedisMirrorDivergenceTotal.Load(); got != 1 {
		t.Fatalf("expected 1 divergence, got %d", got)
	}
}

func TestMirroredRelayDoesNotMirrorDaemonRuntimeEvents(t *testing.T) {
	primary := &recordingManagedRelay{nodeID: "primary"}
	mirror := &recordingManagedRelay{nodeID: "mirror"}
	relay := NewMirroredRelay(primary, mirror)

	if err := relay.PublishWithID(ScopeDaemonRuntime, "task-1", "", []byte(`{"type":"daemon:task_available"}`), "event-1"); err != nil {
		t.Fatalf("PublishWithID: %v", err)
	}

	if len(primary.calls) != 1 {
		t.Fatalf("expected primary publish call, got %d", len(primary.calls))
	}
	if len(mirror.calls) != 0 {
		t.Fatalf("expected daemon runtime event not to hit mirror, got %d calls", len(mirror.calls))
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
	nodeID     string
	publishErr error
	calls      []relayPublishCall
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

func (r *recordingManagedRelay) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	r.calls = append(r.calls, relayPublishCall{
		scopeType: scopeType,
		scopeID:   scopeID,
		exclude:   exclude,
		frame:     string(frame),
		eventID:   id,
	})
	return r.publishErr
}
