package feishu_test

// TC-adapt-6 (PRD E6): the feishu adapter must recognise the
// `im.message.recalled_v1` event type and surface it as a typed
// InboundEvent so the inbound pipeline's dispatcher can render the
// "上游消息已撤回" template without touching IssueFacade /
// CommentFacade.
//
// These tests pin two layers:
//
//   * normaliseEvent (package-internal) emits an InboundEvent with
//     Type == port.EventTypeMessageRecalled and the recalled
//     message_id populated, so a future audit/logging step can correlate
//     the recall with the original ingest.
//   * the adapter pump forwards the recalled event onto Events() — i.e.
//     it is NOT dropped at the adapter layer (which would silently
//     swallow the upstream signal).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// recallPayload is the minimal `im.message.recalled_v1` JSON Feishu
// sends. Only the fields the adapter reads are populated.
const recallPayload = `{
    "schema": "2.0",
    "header": {"event_id": "evt_recall_1", "event_type": "im.message.recalled_v1"},
    "event": {
        "message_id": "om_msg_999",
        "chat_id": "oc_001",
        "recall_time": "1700000050",
        "recall_type": "message_owner"
    }
}`

// TC-adapt-6 (a) — adapter surfaces the recall event on Events().
func TestAdapter_RecallEvent_SurfacedOnEvents(t *testing.T) {
	t.Parallel()

	fake := newFakeFeishuClient("ou_bot_xxx")
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Disconnect(context.Background()) })

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_recall_1",
		EventType: "im.message.recalled_v1",
		Payload:   json.RawMessage(recallPayload),
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() closed before delivering recall event")
		}
		if ev.Type != port.EventTypeMessageRecalled {
			t.Errorf("Type = %q, want %q", ev.Type, port.EventTypeMessageRecalled)
		}
		if ev.EventID != "evt_recall_1" {
			t.Errorf("EventID = %q, want %q", ev.EventID, "evt_recall_1")
		}
		if ev.MessageID != "om_msg_999" {
			t.Errorf("MessageID = %q, want %q (the adapter must surface the recalled message id so the dispatcher can correlate)", ev.MessageID, "om_msg_999")
		}
		if ev.ChatID != "oc_001" {
			t.Errorf("ChatID = %q, want %q", ev.ChatID, "oc_001")
		}
		if ev.ChannelName != "feishu" {
			t.Errorf("ChannelName = %q, want %q", ev.ChannelName, "feishu")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recall event was dropped at the adapter layer (expected to be surfaced on Events())")
	}
}
