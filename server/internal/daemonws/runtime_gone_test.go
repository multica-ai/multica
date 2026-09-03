package daemonws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestNotifyRuntimeGone(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	client := attachDaemonTestClient(hub, "runtime-1")

	hub.NotifyRuntimeGone("runtime-1")

	payload := readRuntimeGoneFrame(t, client.send)
	if payload.RuntimeID != "runtime-1" {
		t.Fatalf("runtime id = %q, want runtime-1", payload.RuntimeID)
	}
	if payload.Status != protocol.HeartbeatStatusRuntimeGone || !payload.RuntimeGone {
		t.Fatalf("payload = %+v, want runtime_gone acknowledgement", payload)
	}
}

func TestRelayNotifierPublishesAndDeliversRuntimeGone(t *testing.T) {
	M.Reset()
	defer M.Reset()

	relay := &recordingRelayPublisher{}
	NewRelayNotifier(nil, relay).NotifyRuntimeGone("runtime-1")

	if relay.scopeType != realtime.ScopeDaemonRuntime || relay.scopeID != "runtime-1" {
		t.Fatalf("relay scope = %q/%q, want daemon-runtime/runtime-1", relay.scopeType, relay.scopeID)
	}
	if relay.eventID == "" {
		t.Fatal("expected event id")
	}

	remoteHub := NewHub()
	remoteClient := attachDaemonTestClient(remoteHub, "runtime-1")
	remoteHub.DeliverDaemonRuntime(relay.scopeID, relay.frame, relay.eventID)
	payload := readRuntimeGoneFrame(t, remoteClient.send)
	if payload.RuntimeID != "runtime-1" || payload.Status != protocol.HeartbeatStatusRuntimeGone || !payload.RuntimeGone {
		t.Fatalf("payload = %+v, want relayed runtime_gone acknowledgement", payload)
	}

	remoteHub.DeliverDaemonRuntime(relay.scopeID, relay.frame, relay.eventID)
	select {
	case duplicate := <-remoteClient.send:
		t.Fatalf("expected duplicate relay event to be dropped, got %s", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRelayNotifierDedupsRuntimeGoneLoopback(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	client := attachDaemonTestClient(hub, "runtime-1")
	relay := &localFirstDaemonRelayPublisher{t: t, client: client}
	NewRelayNotifier(hub, relay).NotifyRuntimeGone("runtime-1")

	if !relay.called || relay.eventID == "" {
		t.Fatal("expected local delivery followed by relay publish")
	}
	if payload := decodeRuntimeGoneFrame(t, relay.localFrame); !payload.RuntimeGone {
		t.Fatalf("local payload = %+v, want runtime_gone acknowledgement", payload)
	}

	hub.DeliverDaemonRuntime(relay.scopeID, relay.frame, relay.eventID)
	select {
	case duplicate := <-client.send:
		t.Fatalf("expected Redis loopback to be deduped, got %s", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
}

func readRuntimeGoneFrame(t *testing.T, frames <-chan []byte) protocol.DaemonHeartbeatAckPayload {
	t.Helper()
	select {
	case raw := <-frames:
		return decodeRuntimeGoneFrame(t, raw)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime_gone frame")
		return protocol.DaemonHeartbeatAckPayload{}
	}
}

func decodeRuntimeGoneFrame(t *testing.T, raw []byte) protocol.DaemonHeartbeatAckPayload {
	t.Helper()
	var msg protocol.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg.Type != protocol.EventDaemonHeartbeatAck {
		t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonHeartbeatAck)
	}
	var payload protocol.DaemonHeartbeatAckPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}
