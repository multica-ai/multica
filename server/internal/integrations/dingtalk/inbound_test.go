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
			name: "richText flattens text runs",
			data: botCallbackData{
				ConversationID: "cid", MsgID: "m4", SenderStaffID: "s", ConversationType: "1", Msgtype: "richText",
				Content: richTextContent{RichText: []richTextNode{
					{Text: "给 FDE教练 发一个任务，"},
					{Text: "让他打印当前工作环境。\n完成后告诉我"},
				}},
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Type != channel.MsgTypeText {
					t.Errorf("Type = %v, want text (flattened richText is actionable)", msg.Type)
				}
				want := "给 FDE教练 发一个任务，让他打印当前工作环境。\n完成后告诉我"
				if msg.Text != want {
					t.Errorf("Text = %q, want %q", msg.Text, want)
				}
			},
		},
		{
			name: "richText with picture keeps placeholder",
			data: botCallbackData{
				ConversationID: "cid", MsgID: "m5", SenderStaffID: "s", ConversationType: "1", Msgtype: "richText",
				Content: richTextContent{RichText: []richTextNode{
					{Text: "看下这个报错"},
					{Type: "picture"},
				}},
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Type != channel.MsgTypeText {
					t.Errorf("Type = %v, want text", msg.Type)
				}
				if msg.Text != "看下这个报错[Image]" {
					t.Errorf("Text = %q", msg.Text)
				}
			},
		},
		{
			name: "richText with no extractable text maps to unknown",
			data: botCallbackData{
				ConversationID: "cid", MsgID: "m6", SenderStaffID: "s", ConversationType: "1", Msgtype: "richText",
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if msg.Type != channel.MsgTypeUnknown {
					t.Errorf("Type = %v, want unknown", msg.Type)
				}
				if msg.Text != "" {
					t.Errorf("Text = %q, want empty", msg.Text)
				}
			},
		},
		{
			name: "richText /new forces fresh session",
			data: botCallbackData{
				ConversationID: "cid", MsgID: "m7", SenderStaffID: "s", ConversationType: "1", Msgtype: "richText",
				Content: richTextContent{RichText: []richTextNode{
					{Text: "/new 重新来"},
				}},
			},
			ok: true,
			check: func(t *testing.T, msg channel.InboundMessage) {
				if !msg.ForceFresh {
					t.Error("ForceFresh = false, want true")
				}
				if msg.Text != "重新来" {
					t.Errorf("Text = %q", msg.Text)
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

// TestDingtalkMessageBodyAndTitle covers the group-context enrichment: the
// stored body carries WHO is talking and in WHICH group (the agent's chat
// prompt is nothing but concatenated bodies), and the session title override
// carries the real group name. DMs stay bare on both counts.
func TestDingtalkMessageBodyAndTitle(t *testing.T) {
	group, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidG==", MsgID: "m1", SenderStaffID: "s1",
		ConversationType: "2", Msgtype: "text",
		SenderNick: "张三", ConversationTitle: "项目群",
		Text: struct {
			Content string `json:"content"`
		}{Content: "帮我看下部署状态"},
	}, "c")
	if got := dingtalkMessageBody(group); got != "[张三 @ 项目群]: 帮我看下部署状态" {
		t.Errorf("group body = %q", got)
	}
	if got := dingtalkSessionTitle(group); got != "项目群" {
		t.Errorf("group title = %q", got)
	}

	// Group without a conversation title: speaker label only, static title.
	noTitle, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidG==", MsgID: "m2", SenderStaffID: "s1",
		ConversationType: "2", Msgtype: "text", SenderNick: "张三",
		Text: struct {
			Content string `json:"content"`
		}{Content: "hi"},
	}, "c")
	if got := dingtalkMessageBody(noTitle); got != "[张三]: hi" {
		t.Errorf("no-title body = %q", got)
	}
	if got := dingtalkSessionTitle(noTitle); got != "" {
		t.Errorf("no-title title = %q, want empty (static fallback)", got)
	}

	// DM: bare body, no title override.
	dm, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidD==", MsgID: "m3", SenderStaffID: "s1",
		ConversationType: "1", Msgtype: "text", SenderNick: "张三",
		Text: struct {
			Content string `json:"content"`
		}{Content: "hi"},
	}, "c")
	if got := dingtalkMessageBody(dm); got != "hi" {
		t.Errorf("dm body = %q, want bare text", got)
	}
	if got := dingtalkSessionTitle(dm); got != "" {
		t.Errorf("dm title = %q, want empty", got)
	}

	// Empty text (pure-media) must NOT gain a dangling speaker label —
	// the daemon's prompt builder skips empty contents.
	empty, _ := inboundFromBotCallback(botCallbackData{
		ConversationID: "cidG==", MsgID: "m4", SenderStaffID: "s1",
		ConversationType: "2", Msgtype: "picture", SenderNick: "张三", ConversationTitle: "项目群",
	}, "c")
	if got := dingtalkMessageBody(empty); got != "" {
		t.Errorf("empty body = %q, want empty", got)
	}
}
