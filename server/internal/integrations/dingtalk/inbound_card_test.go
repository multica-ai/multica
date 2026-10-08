package dingtalk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Sanitized real community DingTalk callback captured on 2026-09-14. UNKNOWN
// nodes include serialized layout data; the readable answer is carried by RICHTEXT children.
func TestInboundQuotedCardObservedSnapshot(t *testing.T) {
	wire, err := os.ReadFile("testdata/quoted_interactive_card.json")
	if err != nil {
		t.Fatal(err)
	}
	var cb botCallbackData
	if err := json.Unmarshal(wire, &cb); err != nil {
		t.Fatal(err)
	}
	msg, ok := inboundFromCallback(&cb, "app")
	want := "> Bananas. Literal text: . Link: https://example.com/a_(b)?x=a_b+c&y=2#part_2\n> [quoted content unavailable]\n\nexplain"
	if !ok || msg.Text != want {
		t.Fatalf("quoted body = %q, want %q (ok=%v)", msg.Text, want, ok)
	}
	if msg.CommandText != "explain" || msg.ForceFresh || !msg.HasSelectedContext || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "selected-bot-message" {
		t.Fatalf("quote changed current instruction or routing: %+v", msg)
	}
}

func TestInboundQuotedCardSnapshotBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, card, want string }{
		{"preview", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"Multica has replied."}]}]`, "Multica has replied."},
		{"literal text", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"/clear a || b "},{"elementType":"TEXT","value":"{\"text\":\"literal\"}"}]}]`, "[quoted content unavailable]\n{\"text\":\"literal\"}"},
		{"blocks", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"one"}]},{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"two"}]}]`, "one\n\ntwo"},
		{"bad neighbor", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"before"},42,{"elementType":"TEXT","value":{}},{"elementType":"TEXT","value":"after"}]}]`, "before\n[quoted content unavailable]\nafter"},
		{"unknown wrapper", `[{"elementType":"UNKNOWN","children":[{"elementType":"TEXT","value":"not verified"}]}]`, "[quoted content unavailable]"},
		{"wrong children", `[{"elementType":"RICHTEXT","children":{}}]`, "[quoted content unavailable]"},
		{"skip source and layout", `[{"elementType":"RICHTEXT","children":[{"elementType":"UNKNOWN","value":"source /clear"},{"elementType":"UNKNOWN","value":"{}"},{"elementType":"TEXT","value":"answer"}]}]`, "answer"},
		{"missing image", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT","value":"before"},{"elementType":"IMAGE","downloadCode":42},{"elementType":"TEXT","value":"after"}]}]`, "before\n[Image]\nafter"},
		{"unknown-only", `[{"elementType":"RICHTEXT","children":[{"elementType":"UNKNOWN","value":"{}"}]}]`, "[quoted content unavailable]"},
		{"empty", `[]`, "[quoted content unavailable]"},
		{"null", `null`, "[quoted content unavailable]"},
		{"invalid blocks", `[42,{"elementType":"RICHTEXT","children":[]}]`, "[quoted content unavailable]"},
		{"missing text values", `[{"elementType":"RICHTEXT","children":[{"elementType":"TEXT"},{"elementType":"TEXT","value":null},{"elementType":"TEXT","value":""}]}]`, "[quoted content unavailable]"},
		{"unsupported child", `[{"elementType":"RICHTEXT","children":[{"elementType":"VIDEO","value":"opaque"},{"elementType":"TEXT","value":"after"}]}]`, "[quoted content unavailable]\nafter"},
		{"leading images", `[{"elementType":"RICHTEXT","children":[{"elementType":"IMAGE"},{"elementType":"IMAGE"},{"elementType":"TEXT","value":"after"}]}]`, "[Image]\n[Image]\nafter"},
		{"template map", `{"cardData":{"cardParamMap":{"text":"not verified"}}}`, "[quoted content unavailable]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cb botCallbackData
			wire := `{"senderStaffId":"sender","conversationType":"1","msgtype":"text","text":{"content":"explain","repliedMsg":{"msgType":"interactiveCard","content":{"cardContent":` + tc.card + `}}}}`
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			want := "> " + strings.ReplaceAll(tc.want, "\n", "\n> ") + "\n\nexplain"
			want = strings.ReplaceAll(want, "\n> \n", "\n>\n")
			if !ok || msg.Text != want || msg.CommandText != "explain" || msg.ForceFresh {
				t.Fatalf("got %+v, want %q", msg, want)
			}
		})
	}
}

// Shapes from the four paired probe traces supplied on 2026-09-14. Repeat the
// selection under content for current richText messages, as the later captures
// demonstrate; the selected message kind is independent of its container.
func TestInboundQuotedBotChannelFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/quoted_bot_channels.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Callback botCallbackData `json:"callback"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		for _, currentKind := range []string{"text", "richText"} {
			t.Run(tc.Name+"/"+currentKind, func(t *testing.T) {
				cb := tc.Callback
				if currentKind == "richText" {
					cb.Msgtype = currentKind
					cb.Content, _ = json.Marshal(map[string]any{"richText": []any{map[string]string{"text": "引用测试"}}, "isReplyMsg": true, "repliedMsg": cb.Text.RepliedMsg})
					cb.Text = botCallbackText{}
				}
				msg, ok := inboundFromCallback(&cb, "app")
				if !ok || msg.Text != "> DingTalk source probe 001.\n\n引用测试" || msg.CommandText != "引用测试" || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "selected" {
					t.Fatalf("channel projection differs: %+v", msg)
				}
			})
		}
	}
}

func TestInboundCardInlineAndMediaOrder(t *testing.T) {
	var cb botCallbackData
	wire := `{"senderStaffId":"sender","conversationType":"1","msgtype":"richText","content":{"richText":[{"text":"current"},{"type":"picture","downloadCode":"current-code"}],"repliedMsg":{"msgType":"interactiveCard","content":{"cardContent":[{"elementType":"RICHTEXT","children":[{"elementType":"UNKNOWN","value":"source"},{"elementType":"TEXT","value":"一只"},{"elementType":"TEXT","value":"木质调色板"},{"elementType":"TEXT","value":"，literal [Image]"},{"elementType":"IMAGE","downloadCode":"selected-code"},{"elementType":"TEXT","value":"after"}]}]}}}}`
	if err := json.Unmarshal([]byte(wire), &cb); err != nil {
		t.Fatal(err)
	}
	msg, ok := inboundFromCallback(&cb, "app")
	if !ok || !strings.Contains(msg.Text, "一只木质调色板，literal [Image]\n> [Image]\n> after") || strings.Contains(msg.Text, "unavailable") {
		t.Fatalf("card inline/media ordering: %q", msg.Text)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 1 || raw.Media[0].Ref != "current-code" || raw.Media[0].InlineIndex != 2 {
		t.Fatalf("media association lost: %+v (%v)", raw, err)
	}
}

// Captured community callbacks retain LINK values even when the visible quote
// preview is truncated. Source/layout values and sender identity are sanitized.
func TestInboundQuotedCardCapturedLinks(t *testing.T) {
	for _, kind := range []string{"private", "group"} {
		t.Run(kind, func(t *testing.T) {
			wire, err := os.ReadFile("testdata/quoted_card_link_" + kind + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var cb botCallbackData
			if err := json.Unmarshal(wire, &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			want := "> DingTalk quote testParagraph one: Apples are red.Paragraph two: Bananas are yellow. Marker: QUOTE-CONTEXT-7429.Literal HTML: keep this visible\n> [quoted content unavailable]\n> Link: https://example.com/a_(b)?x=a_b+c&y=2#part_2\n> [quoted content unavailable]\n> Paragraph three: Cherries are sweet.\n\nQUOTE-CAPTURE-7429"
			if !ok || msg.Text != want {
				t.Fatalf("got %q (ok=%v), want %q", msg.Text, ok, want)
			}
			if msg.CommandText != "QUOTE-CAPTURE-7429" || msg.ForceFresh || !msg.HasSelectedContext {
				t.Fatalf("routing changed: %+v", msg)
			}
		})
	}
}

func TestQuotedCardNodeContract(t *testing.T) {
	for _, tc := range []struct{ name, children, want string }{
		{"link", `{"elementType":"LINK","value":"https://example.com/a_(b)?x=a_b+c&y=2#part_2"}`, "https://example.com/a_(b)?x=a_b+c&y=2#part_2"},
		{"mixed order", `{"elementType":"TEXT","value":"before"},{"elementType":"LINK","value":"https://example.com/one"},{"elementType":"LINK","value":"https://example.com/two"},{"elementType":"IMAGE"},{"elementType":"UNKNOWN","value":"layout"},{"elementType":"TEXT","value":"after"}`, "before\nhttps://example.com/one\nhttps://example.com/two\n[Image]\nafter"},
		{"label and suffix", `{"elementType":"TEXT","value":"Link: "},{"elementType":"LINK","value":"https://example.com/a"},{"elementType":"TEXT","value":"suffix"}`, "Link: https://example.com/a\nsuffix"},
		{"filtered text keeps neighbors", `{"elementType":"TEXT","value":"before"},{"elementType":"TEXT","value":"primary || fallback"},{"elementType":"LINK","value":"https://example.com/ok"},{"elementType":"IMAGE"},{"elementType":"TEXT","value":"after"}`, "before\n[quoted content unavailable]\nhttps://example.com/ok\n[Image]\nafter"},
		{"filtered link keeps neighbors", `{"elementType":"TEXT","value":"before"},{"elementType":"LINK","value":"https://example.com/a||b"},{"elementType":"LINK","value":"https://example.com/ok"},{"elementType":"IMAGE"},{"elementType":"TEXT","value":"after"}`, "before\n[quoted content unavailable]\nhttps://example.com/ok\n[Image]\nafter"},
		{"missing link", `{"elementType":"LINK"}`, "[quoted content unavailable]"},
		{"null link", `{"elementType":"LINK","value":null}`, "[quoted content unavailable]"},
		{"object link", `{"elementType":"LINK","value":{"url":"https://example.com"}}`, "[quoted content unavailable]"},
		{"numeric link", `{"elementType":"LINK","value":42}`, "[quoted content unavailable]"},
		{"empty link", `{"elementType":"LINK","value":""}`, "[quoted content unavailable]"},
		{"blank link", `{"elementType":"LINK","value":"  "}`, "[quoted content unavailable]"},
		{"future node", `{"elementType":"FUTURE_NODE","value":"do not guess"},{"elementType":"TEXT","value":"after"}`, "[quoted content unavailable]\nafter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := json.RawMessage(`[{"elementType":"RICHTEXT","children":[` + tc.children + `]}]`)
			if got := renderDingTalkQuotedCard(card); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
