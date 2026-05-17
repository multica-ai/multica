package inbound

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestChannelBindURLIncludesProviderAndConnection(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example")

	raw := ChannelBindURL("chat", "plain token", "slack", "slack-team-a")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse bind url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "app.example" || parsed.Path != "/bind" {
		t.Fatalf("url = %s", raw)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"kind":          "chat",
		"token":         "plain token",
		"provider":      "slack",
		"connection_id": "slack-team-a",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestUserIdentityBindStep_BypassesRecallEvent(t *testing.T) {
	step := NewUserIdentityBindStep(nil, nil, nil)
	evt := port.InboundEvent{
		Type:      port.EventTypeMessageRecalled,
		EventID:   "evt-recall-1",
		ChatID:    "chat-1",
		MessageID: "om_msg_999",
	}

	_, decision, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if decision != DecisionContinue {
		t.Fatalf("decision = %v, want Continue", decision)
	}
}

func TestChatBindCommandStepSkipsAlreadyBoundChat(t *testing.T) {
	reply := &recordingBindReplySink{}
	step := NewChatBindCommandStep(nil, reply, nil, existingChatBindingLookup{})

	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "feishu-prod",
		ChatID:              "oc_bound",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_user",
		Text:                "/bind",
	}

	_, decision, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if decision != DecisionSkip {
		t.Fatalf("decision = %v, want %v", decision, DecisionSkip)
	}
	if len(reply.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(reply.messages))
	}
	if reply.messages[0].Text == "" {
		t.Fatal("expected already-bound notice")
	}
	if reply.messages[0].Target.Type != port.OutboundTargetChat {
		t.Fatalf("target = %v, want chat", reply.messages[0].Target.Type)
	}
}

type existingChatBindingLookup struct{}

func (existingChatBindingLookup) LookupWorkspaceID(context.Context, string, string) (pgtype.UUID, error) {
	return pgtype.UUID{Valid: true}, nil
}

type recordingBindReplySink struct {
	messages []port.OutboundMessage
}

func (s *recordingBindReplySink) SendText(_ context.Context, _ port.InboundEvent, msg port.OutboundMessage) error {
	s.messages = append(s.messages, msg)
	return nil
}

func (s *recordingBindReplySink) SendRich(_ context.Context, _ port.InboundEvent, _ port.OutboundRichMessage) error {
	return nil
}
