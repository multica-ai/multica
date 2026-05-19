package feishu

import (
	"context"
	"fmt"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestRealClient_StopThenLateEvent_DropsWithoutPanic(t *testing.T) {
	t.Parallel()

	rc := NewRealClient("cli_test", "secret", "", "")
	if err := rc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := rc.handleMessageReceive(context.Background(), &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{EventID: "evt-after-stop"},
		},
	}); err != nil {
		t.Fatalf("handleMessageReceive after Stop: %v", err)
	}

	select {
	case _, ok := <-rc.Subscribe():
		if ok {
			t.Fatal("Subscribe returned an event after Stop")
		}
	default:
		t.Fatal("Subscribe channel should be closed after Stop")
	}
}

func TestRealClient_EnqueueFull_ReturnsError(t *testing.T) {
	t.Parallel()

	rc := NewRealClient("cli_test", "secret", "", "")
	for i := 0; i < cap(rc.events); i++ {
		rc.events <- RawEvent{EventID: fmt.Sprintf("seed-%d", i)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := rc.enqueueRawEvent(ctx, RawEvent{
		EventID:   "evt-full",
		EventType: "im.message.receive_v1",
	})
	if err == nil {
		t.Fatal("enqueueRawEvent should return an error when the event queue stays full")
	}
	if got, want := len(rc.events), cap(rc.events); got != want {
		t.Fatalf("events queue length = %d, want %d", got, want)
	}
}

func TestRealClient_HandleMessageRecalled_EnqueuesRawEvent(t *testing.T) {
	t.Parallel()

	rc := NewRealClient("cli_test", "secret", "", "")
	messageID := "om_recalled"
	chatID := "oc_chat"

	err := rc.handleMessageRecalled(context.Background(), &larkim.P2MessageRecalledV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{EventID: "evt-recall-sdk"},
		},
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: &messageID,
			ChatId:    &chatID,
		},
	})
	if err != nil {
		t.Fatalf("handleMessageRecalled: %v", err)
	}

	select {
	case raw := <-rc.Subscribe():
		if raw.EventID != "evt-recall-sdk" {
			t.Fatalf("EventID = %q, want %q", raw.EventID, "evt-recall-sdk")
		}
		if raw.EventType != "im.message.recalled_v1" {
			t.Fatalf("EventType = %q, want %q", raw.EventType, "im.message.recalled_v1")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recalled raw event")
	}
}
