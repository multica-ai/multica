package wecom

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func inbound(chatType channel.ChatType, chatID, senderID, msgID string) channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: msgID,
		Source: channel.Source{
			ChannelType: TypeWecom,
			ChatID:      chatID,
			ChatType:    chatType,
			SenderID:    senderID,
		},
	}
}

func TestSessionBinding(t *testing.T) {
	cases := []struct {
		name         string
		msg          channel.InboundMessage
		wantKey      string
		wantTargetID string
		wantTargetTy int16
	}{
		{
			name:         "p2p binds on sender userid",
			msg:          inbound(channel.ChatTypeP2P, "ignored", "u1", "m1"),
			wantKey:      "u1",
			wantTargetID: "u1",
			wantTargetTy: TargetChatTypeP2P,
		},
		{
			name:         "group binds on chatid",
			msg:          inbound(channel.ChatTypeGroup, "chat1", "u1", "m2"),
			wantKey:      "chat1",
			wantTargetID: "chat1",
			wantTargetTy: TargetChatTypeGroup,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, cfg, err := SessionBinding(tc.msg)
			if err != nil {
				t.Fatalf("SessionBinding: %v", err)
			}
			if key != tc.wantKey {
				t.Fatalf("binding key = %q, want %q", key, tc.wantKey)
			}
			var got wecomBindingConfig
			if err := json.Unmarshal(cfg, &got); err != nil {
				t.Fatalf("config json: %v", err)
			}
			if got.TargetChatID != tc.wantTargetID || got.TargetChatType != tc.wantTargetTy {
				t.Fatalf("config = %+v, want target %q type %d", got, tc.wantTargetID, tc.wantTargetTy)
			}
		})
	}
}

func TestChatTypeToTargetRoundTrip(t *testing.T) {
	for _, ct := range []channel.ChatType{channel.ChatTypeP2P, channel.ChatTypeGroup} {
		target, err := ChatTypeToTarget(ct)
		if err != nil {
			t.Fatalf("ChatTypeToTarget(%q): %v", ct, err)
		}
		back, err := TargetToChatType(target)
		if err != nil {
			t.Fatalf("TargetToChatType(%d): %v", target, err)
		}
		if back != ct {
			t.Fatalf("round trip %q -> %d -> %q", ct, target, back)
		}
	}
}

func TestNewWecomResolverSet(t *testing.T) {
	set := NewWecomResolverSet(nil, nil, nil, nil)
	if set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		t.Error("resolver set must populate all required resolvers")
	}
	if set.OriginType != originWecomChat {
		t.Fatalf("OriginType = %q, want %q", set.OriginType, originWecomChat)
	}
	if set.Media != nil {
		t.Error("Media must be nil when no media resolver is passed")
	}
}

func TestNewWecomResolverSet_WiresMedia(t *testing.T) {
	media := NewMediaResolver(MediaResolverConfig{})
	set := NewWecomResolverSet(nil, nil, nil, media)
	if set.Media == nil {
		t.Fatal("Media must be populated when a media resolver is passed")
	}
}

func TestDecodeWecomRaw(t *testing.T) {
	raw, _ := json.Marshal(wecomRawEvent{AIBotID: "bot-abc"})
	msg := channel.InboundMessage{Raw: raw}
	got, err := decodeWecomRaw(msg)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AIBotID != "bot-abc" {
		t.Fatalf("AIBotID = %q", got.AIBotID)
	}
}
