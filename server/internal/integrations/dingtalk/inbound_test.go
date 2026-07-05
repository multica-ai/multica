package dingtalk

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromBotCallback(t *testing.T) {
	cases := []struct {
		name  string
		data  botCallbackData
		ok    bool
		check func(t *testing.T, msg channel.InboundMessage)
	}{
		{
			name: "dm text",
			data: botCallbackData{
				ConversationID:   "cidDM==",
				MsgID:            "m1",
				SenderStaffID:    "staff1",
				SenderID:         "$:LWCP_v1:$abc",
				ConversationType: "1",
				Msgtype:          "text",
				SessionWebhook:   "https://x.example/session",
				Text: struct {
					Content string `json:"content"`
				}{Content: "  hi there "},
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Source.ChatType != channel.ChatTypeP2P {
					t.Errorf("ChatType = %v", msg.Source.ChatType)
				}
				if msg.Source.SenderID != "staff1" {
					t.Errorf("SenderID = %q, want staffId preferred", msg.Source.SenderID)
				}
				if msg.Text != "hi there" {
					t.Errorf("Text = %q", msg.Text)
				}
				if !msg.AddressedToBot {
					t.Error("AddressedToBot = false")
				}
				if msg.Type != channel.MsgTypeText {
					t.Errorf("Type = %v", msg.Type)
				}
			},
		},
		{
			name: "group message falls back to senderId when staffId absent",
			data: botCallbackData{
				ConversationID:   "cidGRP==",
				MsgID:            "m2",
				SenderID:         "$:LWCP_v1:$xyz",
				ConversationType: "2",
				Msgtype:          "text",
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Source.ChatType != channel.ChatTypeGroup {
					t.Errorf("ChatType = %v", msg.Source.ChatType)
				}
				if msg.Source.SenderID != "$:LWCP_v1:$xyz" {
					t.Errorf("SenderID = %q", msg.Source.SenderID)
				}
			},
		},
		{
			name: "non-text msgtype maps to unknown",
			data: botCallbackData{
				ConversationID: "cid", MsgID: "m3", SenderStaffID: "s", ConversationType: "1", Msgtype: "picture",
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Type != channel.MsgTypeUnknown {
					t.Errorf("Type = %v, want unknown", msg.Type)
				}
			},
		},
		{
			name: "missing msgId dropped",
			data: botCallbackData{ConversationID: "cid", SenderStaffID: "s", ConversationType: "1"},
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := inboundFromBotCallback(tc.data, "ding_client")
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if msg.Source.ChannelType != TypeDingtalk {
				t.Errorf("ChannelType = %v", msg.Source.ChannelType)
			}
			var raw dingtalkRawEvent
			if err := json.Unmarshal(msg.Raw, &raw); err != nil {
				t.Fatalf("raw decode: %v", err)
			}
			if raw.ClientID != "ding_client" {
				t.Errorf("raw.ClientID = %q", raw.ClientID)
			}
			tc.check(t, msg)
		})
	}
}

func TestDingTalkSessionRouting(t *testing.T) {
	dm, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidDM==", MsgID: "m1", SenderStaffID: "staff1", ConversationType: "1", Msgtype: "text",
	}, "c")
	key, cfg := dingtalkSessionRouting(dm)
	if key != "cidDM==" {
		t.Errorf("dm binding key = %q", key)
	}
	var decoded dingtalkBindingConfig
	if err := json.Unmarshal(cfg, &decoded); err != nil || decoded.SenderStaffID != "staff1" {
		t.Errorf("dm binding config = %s err=%v", cfg, err)
	}

	grp, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidGRP==", MsgID: "m2", SenderStaffID: "staff1", ConversationType: "2", Msgtype: "text",
	}, "c")
	key, cfg = dingtalkSessionRouting(grp)
	if key != "cidGRP==" {
		t.Errorf("group binding key = %q", key)
	}
	var groupCfg dingtalkBindingConfig
	if err := json.Unmarshal(cfg, &groupCfg); err != nil {
		t.Fatalf("group config decode: %v", err)
	}
	// Group sessions must NOT pin a staff id — the reply target is the
	// conversation itself.
	if groupCfg.SenderStaffID != "" {
		t.Errorf("group binding config staff id = %q, want empty", groupCfg.SenderStaffID)
	}
}
