package inbound

import (
	"context"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

const directBusinessUnsupportedReply = "私聊仅用于账号绑定和系统提示，请在已绑定群里处理业务。"

type directChatPolicyStep struct {
	replySink ChannelReplySink
}

func NewDirectChatPolicyStep(replySink ChannelReplySink) Step {
	return &directChatPolicyStep{replySink: replySink}
}

func (*directChatPolicyStep) Name() string { return "direct-chat-policy" }

func (s *directChatPolicyStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if evt.Type != port.EventTypeMessageReceived || evt.ChatType != port.ChatTypeDirect {
		return evt, DecisionContinue, nil
	}
	if strings.TrimSpace(evt.Text) == "/bind" {
		return evt, DecisionContinue, nil
	}
	if s.replySink == nil {
		return evt, DecisionContinue, fmt.Errorf("direct-chat-policy: reply sink is not configured")
	}
	if err := s.replySink.SendText(ctx, evt, port.OutboundMessage{
		Target: port.TargetUser(evt.SenderID),
		Text:   directBusinessUnsupportedReply,
	}); err != nil {
		return evt, DecisionContinue, fmt.Errorf("direct-chat-policy: send reply: %w", err)
	}
	return evt, DecisionSkip, nil
}

var _ Step = (*directChatPolicyStep)(nil)
