package inbound

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

type recordingDirectReplySink struct {
	messages []port.OutboundMessage
}

func (r *recordingDirectReplySink) SendText(_ context.Context, _ port.InboundEvent, msg port.OutboundMessage) error {
	r.messages = append(r.messages, msg)
	return nil
}

func (r *recordingDirectReplySink) SendRich(_ context.Context, _ port.InboundEvent, _ port.OutboundRichMessage) error {
	return nil
}

func TestDirectChatPolicyStep_DirectBusinessSkipsWithReply(t *testing.T) {
	t.Parallel()

	sink := &recordingDirectReplySink{}
	step := NewDirectChatPolicyStep(sink)
	evt := port.InboundEvent{
		Type:     port.EventTypeMessageReceived,
		ChatType: port.ChatTypeDirect,
		SenderID: "ou_1",
		Text:     "创建一个 issue",
	}

	_, decision, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if decision != DecisionSkip {
		t.Fatalf("decision = %v, want skip", decision)
	}
	if len(sink.messages) != 1 || sink.messages[0].Target.Type != port.OutboundTargetUser {
		t.Fatalf("messages = %#v, want one direct reply", sink.messages)
	}
}

func TestDirectChatPolicyStep_AllowsGroupAndDirectBind(t *testing.T) {
	t.Parallel()

	sink := &recordingDirectReplySink{}
	step := NewDirectChatPolicyStep(sink)
	cases := []port.InboundEvent{
		{Type: port.EventTypeMessageReceived, ChatType: port.ChatTypeGroup, Text: "创建 issue"},
		{Type: port.EventTypeMessageReceived, ChatType: port.ChatTypeDirect, Text: "/bind"},
	}

	for _, evt := range cases {
		_, decision, err := step.Run(context.Background(), evt)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if decision != DecisionContinue {
			t.Fatalf("decision = %v, want continue", decision)
		}
	}
	if len(sink.messages) != 0 {
		t.Fatalf("messages = %#v, want none", sink.messages)
	}
}
