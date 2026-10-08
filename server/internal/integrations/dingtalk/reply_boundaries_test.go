package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestAckNotifierCredentialFailuresAreOptional(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform any
		decrypt  Decrypter
	}{
		{name: "missing installation"},
		{name: "malformed config", platform: db.ChannelInstallation{Config: []byte(`{`)}},
		{name: "decryption failure", platform: db.ChannelInstallation{Config: []byte(`{"app_id":"app","app_secret_encrypted":"eA=="}`)}, decrypt: func([]byte) ([]byte, error) { return nil, errors.New("unavailable key") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			n := NewAckNotifier(NewClient(nil, d.srv.URL), tc.decrypt, nil, nil)
			n.OnIngested(context.Background(), engine.ResolvedInstallation{Platform: tc.platform}, groupReactionMessage("source"), sessionUUID(91))
			n.OnSettled(context.Background(), sessionUUID(91))
			if len(d.sendBodies) != 0 || len(n.active) != 0 {
				t.Fatal("unusable credentials sent a reaction or prevented local cleanup")
			}
		})
	}
}

func TestCommandReplyFailuresStayOptional(t *testing.T) {
	for _, outcome := range []engine.Outcome{engine.OutcomeFreshPending, engine.OutcomeChatStarted} {
		t.Run(string(outcome), func(t *testing.T) {
			d := newDingtalkSendServer(t)
			r := NewOutboundReplier(OutboundReplierConfig{Client: NewClient(nil, d.srv.URL)})
			r.Reply(context.Background(), engine.ResolvedInstallation{}, groupReactionMessage("source"), engine.Result{Outcome: outcome})
			if len(d.sendBodies) != 0 {
				t.Fatal("missing credentials sent a command reply")
			}
		})
	}
}

func TestImmediateQuoteWithoutSnapshotDoesNotInventHistory(t *testing.T) {
	for _, msg := range []channel.InboundMessage{
		{Text: "history", ReplyTo: &channel.ReplyCtx{MessageID: "parent"}},
		{Text: "[Image]", Type: channel.MsgTypeImage},
	} {
		if got := dingtalkVisibleQuoteText(msg); got != "" {
			t.Fatalf("missing current text quoted unrelated content: %q", got)
		}
	}
	set := NewDingTalkResolverSet(nil, nil, nil, nil, nil, nil)
	if set.Session == nil {
		t.Fatal("optional notifier disabled session routing")
	}
}

func TestSenderEmptyReplyDoesNotSendQuoteAlone(t *testing.T) {
	d := newDingtalkSendServer(t)
	key, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), sendTarget{ConversationID: "group", QuoteText: "question"}, "")
	if err != nil || key != "" || len(d.sendBodies) != 0 {
		t.Fatalf("empty reply gained a provider side effect: key=%q error=%v", key, err)
	}
}

func TestLegacyAndMixedInputAttribution(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		d := newDingtalkSendServer(t)
		q := deliveryOnlyOutboundQueries{
			delivery:     db.ChannelTaskDelivery{ChannelType: string(TypeDingTalk), ChannelChatID: "group"},
			installation: db.ChannelInstallation{Status: "active", Config: []byte(`{"app_id":"app","app_secret_encrypted":"eA=="}`)},
			task:         db.AgentTaskQueue{ChatInputTaskID: sessionUUID(94)}, channelIngested: true,
			input: []db.ChatMessage{{ID: sessionUUID(95), ChannelIngested: true, Content: "owned question"}, {Content: "local-only message"}},
		}
		if legacy {
			q.task.ChatInputTaskID.Valid = false
		}
		o := NewOutbound(q, nil, NewClient(nil, d.srv.URL), nil, nil)
		if err := o.processEvent(context.Background(), events.Event{Type: protocol.EventChatDone, TaskID: "11111111-1111-1111-1111-111111111111", ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: "answer"}}); err != nil {
			t.Fatal(err)
		}
		var param markdownParam
		if err := json.Unmarshal([]byte(d.lastBody["msgParam"].(string)), &param); err != nil {
			t.Fatal(err)
		}
		want := "> owned question\n\n---\n\nanswer"
		if legacy {
			want = "answer"
		}
		if param.Text != want {
			t.Fatalf("wrong input attribution: %q", param.Text)
		}
	}
}
