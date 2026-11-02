package dingtalk

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromCallback_QuotedAuthorUsesDisplayNameOnly(t *testing.T) {
	const opaqueID = "$:LWCP_v1:$dasU4wKakkQml8Lzp3xPEQ=="
	for _, location := range []string{"text", "content"} {
		for _, tc := range []struct {
			name, senderID, senderNick, author string
		}{
			{name: "opaque ID without nickname", senderID: opaqueID},
			{name: "whitespace nickname", senderID: opaqueID, senderNick: " \t\n "},
			{name: "plain ID without nickname", senderID: "platform-author-id"},
			{name: "missing author"},
			{name: "readable nickname", senderID: opaqueID, senderNick: " Alice ", author: "> **Alice:**\n>\n"},
			{
				name: "nickname remains escaped plain text", senderID: opaqueID,
				senderNick: " Alice [Image] & **team**\n ",
				author:     "> **Alice \\[Image\\] &amp; \\*\\*team\\*\\*:**\n>\n",
			},
		} {
			t.Run(location+"/"+tc.name, func(t *testing.T) {
				cb := textCallback(convTypeP2P, false)
				cb.Text.Content = "inspect " + opaqueID
				reply := &botCallbackRepliedMessage{
					MsgType: "text", MsgId: "quoted-message", SenderId: tc.senderID, SenderNick: tc.senderNick,
					Content: botCallbackRepliedContent{Text: "selected " + opaqueID},
				}
				if location == "text" {
					cb.Text.IsReplyMsg = true
					cb.Text.RepliedMsg = reply
				} else {
					content, err := json.Marshal(botCallbackReplyMetadata{IsReplyMsg: true, RepliedMsg: reply})
					if err != nil {
						t.Fatal(err)
					}
					cb.Content = content
				}

				msg, ok := inboundFromCallback(cb, "app-key")
				want := tc.author + "> selected " + opaqueID + "\n\ninspect " + opaqueID
				if !ok || msg.Text != want {
					t.Fatalf("quoted body = %q, ok=%v; want %q", msg.Text, ok, want)
				}
				if msg.CommandText != cb.Text.Content || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-message" {
					t.Fatalf("current command or reply identity changed: %+v", msg)
				}
			})
		}
	}
}

func TestInboundFromCallback_QuotedAuthorOmissionPreservesMediaLayout(t *testing.T) {
	for _, location := range []string{"text", "content"} {
		t.Run(location, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "richText"
			reply := &botCallbackRepliedMessage{
				MsgType: "richText", MsgId: "quoted-message", SenderId: "$:LWCP_v1:quoted-user",
				Content: botCallbackRepliedContent{RichText: richTextItems{
					{Text: "111"}, {Type: "picture", DownloadCode: "quoted-picture"}, {Text: "quoted caption"},
				}},
			}
			content := map[string]any{"richText": richTextItems{
				{Text: "/new 222"}, {Type: "picture", DownloadCode: "current-picture"},
			}}
			if location == "text" {
				cb.Text.IsReplyMsg = true
				cb.Text.RepliedMsg = reply
			} else {
				content["isReplyMsg"] = true
				content["repliedMsg"] = reply
			}
			encoded, err := json.Marshal(content)
			if err != nil {
				t.Fatal(err)
			}
			cb.Content = encoded

			msg, ok := inboundFromCallback(cb, "app-key")
			const want = "> 111\n> [Image]\n> quoted caption\n\n222\n[Image]"
			if !ok || msg.Text != want || msg.Type != channel.MsgTypeImage {
				t.Fatalf("quoted/current media layout = %q, type=%v, ok=%v; want %q", msg.Text, msg.Type, ok, want)
			}
			if msg.CommandText != "/new 222" || msg.ForceFresh || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-message" {
				t.Fatalf("current command or reply identity changed: %+v", msg)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 2 {
				t.Fatalf("media = %+v, err=%v", raw.Media, err)
			}
			if raw.Media[0].Ref != "quoted-picture" || raw.Media[0].InlineIndex != 0 ||
				raw.Media[1].Ref != "current-picture" || raw.Media[1].InlineIndex != 1 {
				t.Fatalf("quoted/current media identity or order changed: %+v", raw.Media)
			}
		})
	}
}
