package daemonws

import (
	"encoding/json"
	"errors"
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
	if M.RuntimeGoneDeliveredHit.Load() != 1 {
		t.Fatalf("runtime-gone delivered hit metric = %d, want 1", M.RuntimeGoneDeliveredHit.Load())
	}
	if M.WakeupDeliveredHit.Load() != 0 || M.WakeupDeliveredMiss.Load() != 0 {
		t.Fatalf("runtime-gone polluted wakeup delivery metrics: hit=%d miss=%d", M.WakeupDeliveredHit.Load(), M.WakeupDeliveredMiss.Load())
	}
}

func TestNotifyRuntimeGoneMissingConnectionDoesNotPolluteWakeupMetrics(t *testing.T) {
	M.Reset()
	defer M.Reset()

	NewHub().NotifyRuntimeGone("offline-runtime")

	if M.RuntimeGoneDeliveredHit.Load() != 0 || M.RuntimeGoneDeliveredMiss.Load() != 1 {
		t.Fatalf("runtime-gone delivery metrics: hit=%d miss=%d, want 0/1", M.RuntimeGoneDeliveredHit.Load(), M.RuntimeGoneDeliveredMiss.Load())
	}
	if M.WakeupDeliveredHit.Load() != 0 || M.WakeupDeliveredMiss.Load() != 0 {
		t.Fatalf("runtime-gone polluted wakeup delivery metrics: hit=%d miss=%d", M.WakeupDeliveredHit.Load(), M.WakeupDeliveredMiss.Load())
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
	if M.RuntimeGonePublishedTotal.Load() != 1 || M.RuntimeGonePublishErrors.Load() != 0 {
		t.Fatalf("runtime-gone publish metrics: published=%d errors=%d, want 1/0", M.RuntimeGonePublishedTotal.Load(), M.RuntimeGonePublishErrors.Load())
	}
	if M.WakeupPublishedTotal.Load() != 0 || M.WakeupPublishErrors.Load() != 0 {
		t.Fatalf("runtime-gone polluted wakeup publish metrics: published=%d errors=%d", M.WakeupPublishedTotal.Load(), M.WakeupPublishErrors.Load())
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
	if M.RuntimeGoneDeliveredHit.Load() != 1 || M.RuntimeGoneDeliveredMiss.Load() != 0 {
		t.Fatalf("runtime-gone delivery metrics after loopback: hit=%d miss=%d, want 1/0", M.RuntimeGoneDeliveredHit.Load(), M.RuntimeGoneDeliveredMiss.Load())
	}
	if M.RuntimeGoneReceivedTotal.Load() != 2 || M.WakeupReceivedTotal.Load() != 0 {
		t.Fatalf("relay receive metrics: runtime-gone=%d wakeup=%d, want 2/0", M.RuntimeGoneReceivedTotal.Load(), M.WakeupReceivedTotal.Load())
	}
	if M.WakeupDeliveredHit.Load() != 0 || M.WakeupDeliveredMiss.Load() != 0 {
		t.Fatalf("runtime-gone polluted wakeup delivery metrics: hit=%d miss=%d", M.WakeupDeliveredHit.Load(), M.WakeupDeliveredMiss.Load())
	}
}

func TestRelayNotifierRuntimeGonePublishErrorDoesNotPolluteWakeupMetrics(t *testing.T) {
	M.Reset()
	defer M.Reset()

	NewRelayNotifier(nil, failingRelayPublisher{}).NotifyRuntimeGone("runtime-1")

	if M.RuntimeGonePublishedTotal.Load() != 0 || M.RuntimeGonePublishErrors.Load() != 1 {
		t.Fatalf("runtime-gone publish metrics: published=%d errors=%d, want 0/1", M.RuntimeGonePublishedTotal.Load(), M.RuntimeGonePublishErrors.Load())
	}
	if M.WakeupPublishedTotal.Load() != 0 || M.WakeupPublishErrors.Load() != 0 {
		t.Fatalf("runtime-gone polluted wakeup publish metrics: published=%d errors=%d", M.WakeupPublishedTotal.Load(), M.WakeupPublishErrors.Load())
	}
}

func TestDeliverDaemonRuntimeInvalidRuntimeGoneDoesNotPolluteWakeupMetrics(t *testing.T) {
	M.Reset()
	defer M.Reset()

	frame, err := json.Marshal(protocol.Message{
		Type: protocol.EventDaemonHeartbeatAck,
		Payload: mustMarshalRaw(protocol.DaemonHeartbeatAckPayload{
			RuntimeID: "runtime-1",
			Status:    "invalid",
		}),
	})
	if err != nil {
		t.Fatalf("marshal invalid runtime-gone frame: %v", err)
	}
	NewHub().DeliverDaemonRuntime("runtime-1", frame, "event-1")

	if M.RuntimeGoneReceivedTotal.Load() != 1 || M.RuntimeGoneDeliveredMiss.Load() != 1 {
		t.Fatalf("invalid runtime-gone metrics: received=%d miss=%d, want 1/1", M.RuntimeGoneReceivedTotal.Load(), M.RuntimeGoneDeliveredMiss.Load())
	}
	if M.WakeupReceivedTotal.Load() != 0 || M.WakeupDeliveredMiss.Load() != 0 {
		t.Fatalf("invalid runtime-gone polluted wakeup metrics: received=%d miss=%d", M.WakeupReceivedTotal.Load(), M.WakeupDeliveredMiss.Load())
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

type failingRelayPublisher struct{}

func (failingRelayPublisher) PublishWithID(string, string, string, []byte, string) error {
	return errors.New("injected relay publish failure")
}
