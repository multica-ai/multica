package slack

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func inbound(chatType channel.ChatType, chatID, threadID, msgID string) channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: msgID,
		Source: channel.Source{
			ChannelType: TypeSlack,
			ChatID:      chatID,
			ChatType:    chatType,
			ThreadID:    threadID,
		},
	}
}

func TestSlackSessionRouting(t *testing.T) {
	cases := []struct {
		name           string
		msg            channel.InboundMessage
		wantKey        string
		wantReplyThr   string
		wantChannelCfg string
	}{
		{
			name:           "DM top-level: one session per channel",
			msg:            inbound(channel.ChatTypeP2P, "D1", "", "111.0"),
			wantKey:        "D1",
			wantReplyThr:   "",
			wantChannelCfg: "D1",
		},
		{
			name:           "DM in a thread: still one session per channel, reply into the thread",
			msg:            inbound(channel.ChatTypeP2P, "D1", "100.0", "111.0"),
			wantKey:        "D1",
			wantReplyThr:   "100.0",
			wantChannelCfg: "D1",
		},
		{
			name:           "channel top-level @mention: new thread root = message ts",
			msg:            inbound(channel.ChatTypeGroup, "C1", "", "111.0"),
			wantKey:        "C1:111.0",
			wantReplyThr:   "111.0",
			wantChannelCfg: "C1",
		},
		{
			name:           "channel thread reply: isolated by thread root",
			msg:            inbound(channel.ChatTypeGroup, "C1", "100.0", "222.0"),
			wantKey:        "C1:100.0",
			wantReplyThr:   "100.0",
			wantChannelCfg: "C1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, cfg, reply := slackSessionRouting(tc.msg)
			if key != tc.wantKey {
				t.Errorf("bindingKey = %q, want %q", key, tc.wantKey)
			}
			if reply != tc.wantReplyThr {
				t.Errorf("replyThread = %q, want %q", reply, tc.wantReplyThr)
			}
			var got slackBindingConfig
			if err := json.Unmarshal(cfg, &got); err != nil {
				t.Fatalf("config not valid json: %v", err)
			}
			if got.ChannelID != tc.wantChannelCfg {
				t.Errorf("config.channel_id = %q, want %q (real channel for outbound)", got.ChannelID, tc.wantChannelCfg)
			}
		})
	}
}

// TestSlackThreadIsolation is the resolver-level guard Niko/Elon asked for: two
// @bot threads in the SAME channel must derive DISTINCT session binding keys,
// while a follow-up in the same thread derives the same key.
func TestSlackThreadIsolation(t *testing.T) {
	thread1Root, _, _ := slackSessionRouting(inbound(channel.ChatTypeGroup, "C1", "", "1111.0"))        // @mention starts thread 1
	thread2Root, _, _ := slackSessionRouting(inbound(channel.ChatTypeGroup, "C1", "", "2222.0"))        // @mention starts thread 2
	thread1Reply, _, _ := slackSessionRouting(inbound(channel.ChatTypeGroup, "C1", "1111.0", "3333.0")) // reply in thread 1

	if thread1Root == thread2Root {
		t.Errorf("two @bot threads in one channel must isolate: %q == %q", thread1Root, thread2Root)
	}
	if thread1Reply != thread1Root {
		t.Errorf("a follow-up in thread 1 must reuse thread 1's key: %q != %q", thread1Reply, thread1Root)
	}
}

func TestNewSlackResolverSet(t *testing.T) {
	set := NewSlackResolverSet(nil, nil)
	if set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		t.Error("resolver set must populate all required resolvers")
	}
	if set.OriginType != "slack_chat" {
		t.Errorf("OriginType = %q, want slack_chat", set.OriginType)
	}
}
