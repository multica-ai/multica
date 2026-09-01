package mattermost

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func inboundFor(chatType channel.ChatType, chatID, messageID, threadID string) channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: messageID,
		Source: channel.Source{
			ChannelType: TypeMattermost,
			ChatID:      chatID,
			ChatType:    chatType,
			ThreadID:    threadID,
		},
	}
}

// Session isolation is the contract that decides which Multica chat a message
// lands in, so it is pinned here without a database.
func TestMattermostSessionRouting(t *testing.T) {
	tests := []struct {
		name            string
		msg             channel.InboundMessage
		wantKey         string
		wantReplyThread string
	}{
		{
			name:            "direct message is one continuous session",
			msg:             inboundFor(channel.ChatTypeP2P, "dm1", "p1", ""),
			wantKey:         "dm1",
			wantReplyThread: "",
		},
		{
			name: "direct message inside a thread still keys on the channel",
			// A DM thread does not fork the session; the reply still threads.
			msg:             inboundFor(channel.ChatTypeP2P, "dm1", "p2", "root1"),
			wantKey:         "dm1",
			wantReplyThread: "root1",
		},
		{
			name: "top-level channel mention starts its own session",
			// The post becomes the thread root the bot will reply under.
			msg:             inboundFor(channel.ChatTypeGroup, "chan1", "p1", ""),
			wantKey:         "chan1:p1",
			wantReplyThread: "p1",
		},
		{
			name:            "reply inside a channel thread joins that session",
			msg:             inboundFor(channel.ChatTypeGroup, "chan1", "p2", "root1"),
			wantKey:         "chan1:root1",
			wantReplyThread: "root1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, cfg, replyThread := mattermostSessionRouting(tc.msg)
			if key != tc.wantKey {
				t.Errorf("bindingKey = %q, want %q", key, tc.wantKey)
			}
			if replyThread != tc.wantReplyThread {
				t.Errorf("replyThread = %q, want %q", replyThread, tc.wantReplyThread)
			}
			// The real channel id must survive in the config, because the
			// binding key may be composite and outbound needs the plain id.
			var decoded mattermostBindingConfig
			if err := json.Unmarshal(cfg, &decoded); err != nil {
				t.Fatalf("binding config is not decodable: %v", err)
			}
			if decoded.ChannelID != tc.msg.Source.ChatID {
				t.Errorf("config channel id = %q, want %q", decoded.ChannelID, tc.msg.Source.ChatID)
			}
		})
	}
}

// Two independent threads in one channel must not share a session.
func TestMattermostSessionRoutingIsolatesThreads(t *testing.T) {
	a, _, _ := mattermostSessionRouting(inboundFor(channel.ChatTypeGroup, "chan1", "p1", "rootA"))
	b, _, _ := mattermostSessionRouting(inboundFor(channel.ChatTypeGroup, "chan1", "p2", "rootB"))
	if a == b {
		t.Fatalf("two threads share the binding key %q", a)
	}
}

func TestDecodeMattermostRaw(t *testing.T) {
	raw, err := json.Marshal(mattermostRawEvent{AppID: testAppID, EventType: eventPosted, SenderName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeMattermostRaw(channel.InboundMessage{Raw: raw})
	if err != nil {
		t.Fatalf("decodeMattermostRaw: %v", err)
	}
	if got.AppID != testAppID || got.EventType != eventPosted || got.SenderName != "alice" {
		t.Errorf("raw = %+v, want the round-tripped fields", got)
	}

	if _, err := decodeMattermostRaw(channel.InboundMessage{}); err == nil {
		t.Error("empty Raw accepted, want an error")
	}
	if _, err := decodeMattermostRaw(channel.InboundMessage{Raw: json.RawMessage("{broken")}); err == nil {
		t.Error("malformed Raw accepted, want an error")
	}
}

func TestNewMattermostResolverSetShape(t *testing.T) {
	set := NewMattermostResolverSet(nil, nil, nil)
	if set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		t.Fatalf("resolver set is missing a required stage: %+v", set)
	}
	if set.OriginType != originMattermostChat {
		t.Errorf("OriginType = %q, want %q", set.OriginType, originMattermostChat)
	}
	// Mattermost has no REST typing endpoint, so the seam stays nil rather
	// than holding a typed nil that would panic when the pipeline calls it.
	if set.Typing != nil {
		t.Error("Typing is set, want nil for v1")
	}
	// Media ingest is out of scope for v1.
	if set.Media != nil {
		t.Error("Media is set, want nil for v1")
	}
}

func TestNullText(t *testing.T) {
	if got := nullText(""); got.Valid {
		t.Error("nullText(\"\") is valid, want NULL")
	}
	got := nullText("value")
	if !got.Valid || got.String != "value" {
		t.Errorf("nullText = %+v, want a valid 'value'", got)
	}
}

func TestOutboundTarget(t *testing.T) {
	t.Run("recovers the real channel id from a composite key", func(t *testing.T) {
		cfg, err := json.Marshal(mattermostBindingConfig{ChannelID: "chan1"})
		if err != nil {
			t.Fatal(err)
		}
		channelID, thread := outboundTarget(db.ChannelChatSessionBinding{
			ChannelChatID: "chan1:root9",
			Config:        cfg,
			LastThreadID:  pgtype.Text{String: "root9", Valid: true},
		})
		if channelID != "chan1" {
			t.Errorf("channelID = %q, want chan1 (not the composite key)", channelID)
		}
		if thread != "root9" {
			t.Errorf("thread = %q, want root9", thread)
		}
	})

	t.Run("falls back to the chat id when config is unusable", func(t *testing.T) {
		channelID, thread := outboundTarget(db.ChannelChatSessionBinding{
			ChannelChatID: "dm1",
			Config:        json.RawMessage("{broken"),
		})
		if channelID != "dm1" {
			t.Errorf("channelID = %q, want dm1", channelID)
		}
		if thread != "" {
			t.Errorf("thread = %q, want empty", thread)
		}
	})
}
