package lark

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLarkJSONFrameDecoderTextMessageInP2P(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"type":"event_callback",
		"header":{
			"event_id":"evt-1",
			"event_type":"im.message.receive_v1",
			"app_id":"cli_app_x"
		},
		"event":{
			"sender":{
				"sender_id":{"open_id":"ou_user"},
				"sender_type":"user"
			},
			"message":{
				"message_id":"om_1",
				"chat_id":"oc_1",
				"chat_type":"p2p",
				"message_type":"text",
				"content":"{\"text\":\"hello\"}"
			}
		}
	}`)

	d := NewLarkJSONFrameDecoder()
	msg, ok, err := d.Decode(raw, db.LarkInstallation{BotOpenID: "ou_bot"})
	if err != nil || !ok {
		t.Fatalf("Decode ok=%v err=%v", ok, err)
	}
	if msg.EventID != "evt-1" {
		t.Errorf("EventID = %q", msg.EventID)
	}
	if msg.AppID != "cli_app_x" {
		t.Errorf("AppID = %q", msg.AppID)
	}
	if msg.ChatType != ChatTypeP2P {
		t.Errorf("ChatType = %q", msg.ChatType)
	}
	if msg.MessageID != "om_1" {
		t.Errorf("MessageID = %q", msg.MessageID)
	}
	if msg.SenderOpenID != "ou_user" {
		t.Errorf("SenderOpenID = %q", msg.SenderOpenID)
	}
	if msg.Body != "hello" {
		t.Errorf("Body = %q", msg.Body)
	}
	if msg.AddressedToBot {
		t.Errorf("P2P AddressedToBot should not be true")
	}
}

func TestLarkJSONFrameDecoderGroupMentionDiscrimination(t *testing.T) {
	t.Parallel()
	mkRaw := func(mentionOpenID string) []byte {
		return []byte(`{
			"type":"event_callback",
			"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
			"event":{
				"sender":{"sender_id":{"open_id":"ou_user"}},
				"message":{
					"message_id":"m","chat_id":"c","chat_type":"group",
					"message_type":"text","content":"{\"text\":\"hi\"}",
					"mentions":[{"id":{"open_id":"` + mentionOpenID + `"}}]
				}
			}
		}`)
	}
	d := NewLarkJSONFrameDecoder()

	t.Run("mentions bot", func(t *testing.T) {
		msg, ok, err := d.Decode(mkRaw("ou_bot"), db.LarkInstallation{BotOpenID: "ou_bot"})
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.ChatType != ChatTypeGroup {
			t.Errorf("ChatType = %q", msg.ChatType)
		}
		if !msg.AddressedToBot {
			t.Error("AddressedToBot = false; expected true")
		}
	})

	t.Run("mentions other user", func(t *testing.T) {
		msg, ok, err := d.Decode(mkRaw("ou_other"), db.LarkInstallation{BotOpenID: "ou_bot"})
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.AddressedToBot {
			t.Error("AddressedToBot = true; expected false")
		}
	})
}

// TestLarkJSONFrameDecoderGroupMentionUnionID exercises the MUL-2671
// fix: in a multi-bot group chat the per-app `mentions[].id.open_id`
// is structurally inverted across WS perspectives, so we route on
// `union_id` (the stable, cross-app identifier captured at install
// time) when the installation row knows it. The open_id path remains
// as a transitional fallback for installations that haven't been
// backfilled yet.
func TestLarkJSONFrameDecoderGroupMentionUnionID(t *testing.T) {
	t.Parallel()

	mkRaw := func(mentionOpenID, mentionUnionID string) []byte {
		return []byte(`{
			"type":"event_callback",
			"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
			"event":{
				"sender":{"sender_id":{"open_id":"ou_user"}},
				"message":{
					"message_id":"m","chat_id":"c","chat_type":"group",
					"message_type":"text","content":"{\"text\":\"hi\"}",
					"mentions":[{"id":{"open_id":"` + mentionOpenID + `","union_id":"` + mentionUnionID + `"}}]
				}
			}
		}`)
	}
	d := NewLarkJSONFrameDecoder()
	pgText := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

	t.Run("union_id match wins even when open_id mismatches", func(t *testing.T) {
		// Two-bot group chat, this bot's WS perspective:
		// payload.mentions[0].open_id is the WIRE-form open_id Lark
		// hands us (not equal to our installation's bot_open_id,
		// which is what /bot/v3/info returned), but the union_id is
		// the stable identifier we captured at install. The match
		// must succeed.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot_a_canonical",
			BotUnionID: pgText("on_bot_a_union"),
		}
		msg, ok, err := d.Decode(mkRaw("ou_bot_a_wire", "on_bot_a_union"), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if !msg.AddressedToBot {
			t.Error("AddressedToBot = false; expected true via union_id")
		}
	})

	t.Run("union_id mismatch wins even when open_id matches", func(t *testing.T) {
		// The other bot in the group was @-mentioned; Lark hands
		// THIS bot's WS a payload whose mentions[].id.open_id
		// happens to equal our bot_open_id (the inverse-mapping
		// quirk Bohan's live triage surfaced). The match must NOT
		// fire — union_id is the source of truth.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot_a_canonical",
			BotUnionID: pgText("on_bot_a_union"),
		}
		msg, ok, err := d.Decode(mkRaw("ou_bot_a_canonical", "on_bot_b_union"), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.AddressedToBot {
			t.Error("AddressedToBot = true; expected false because union_id points at the OTHER bot")
		}
	})

	t.Run("falls back to open_id when union_id is unknown", func(t *testing.T) {
		// Pre-backfill installation row: no union_id yet. Decoder
		// must keep working in the single-bot case via the legacy
		// open_id comparison.
		inst := db.LarkInstallation{BotOpenID: "ou_bot_a_canonical"}
		msg, ok, err := d.Decode(mkRaw("ou_bot_a_canonical", "on_anything"), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if !msg.AddressedToBot {
			t.Error("AddressedToBot = false; expected true via legacy open_id fallback")
		}
	})
}

// TestLarkJSONFrameDecoderMentionPlaceholderRewrite covers the body
// cleanup: Lark inlines `@_user_N` placeholders inside the text and
// resolves them via the `mentions` array. We strip the bot's own
// mention (the dispatcher already routes the event), substitute
// other users with `@<displayName>`, and leave the agent with a
// natural-looking message body.
func TestLarkJSONFrameDecoderMentionPlaceholderRewrite(t *testing.T) {
	t.Parallel()
	pgText := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

	mkRaw := func(text, mentionsJSON string) []byte {
		// Lark wraps text in a `{"text": ...}` JSON envelope inside
		// `message.content`; we double-encode below to match wire.
		contentDoc := map[string]string{"text": text}
		contentBytes, _ := json.Marshal(contentDoc)
		contentEsc, _ := json.Marshal(string(contentBytes))
		return []byte(`{
			"type":"event_callback",
			"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
			"event":{
				"sender":{"sender_id":{"open_id":"ou_user"}},
				"message":{
					"message_id":"m","chat_id":"c","chat_type":"group",
					"message_type":"text",
					"content":` + string(contentEsc) + `,
					"mentions":` + mentionsJSON + `
				}
			}
		}`)
	}
	d := NewLarkJSONFrameDecoder()

	t.Run("strips bot self-mention via union_id", func(t *testing.T) {
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		mentions := `[{"key":"@_user_1","name":"My Bot","id":{"open_id":"ou_bot_wire","union_id":"on_bot"}}]`
		msg, ok, err := d.Decode(mkRaw("@_user_1 ping test", mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != "ping test" {
			t.Errorf("Body = %q; want %q", msg.Body, "ping test")
		}
	})

	t.Run("substitutes other-user mention with display name", func(t *testing.T) {
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		mentions := `[
			{"key":"@_user_1","name":"My Bot","id":{"open_id":"ou_bot_wire","union_id":"on_bot"}},
			{"key":"@_user_2","name":"Alice","id":{"open_id":"ou_alice","union_id":"on_alice"}}
		]`
		msg, ok, err := d.Decode(mkRaw("@_user_1 hey @_user_2 take a look", mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != "hey @Alice take a look" {
			t.Errorf("Body = %q; want %q", msg.Body, "hey @Alice take a look")
		}
	})

	t.Run("preserves newlines after stripped mention", func(t *testing.T) {
		// Strip the bot mention + one adjacent space; the newline that
		// follows stays put so the rest of the message keeps its
		// shape. User-typed extra spaces (the double space here) are
		// preserved verbatim — we do not globally collapse whitespace.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		mentions := `[{"key":"@_user_1","name":"My Bot","id":{"open_id":"ou_bot_wire","union_id":"on_bot"}}]`
		msg, ok, err := d.Decode(mkRaw("@_user_1  first line\nsecond line", mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != " first line\nsecond line" {
			t.Errorf("Body = %q; want %q", msg.Body, " first line\nsecond line")
		}
	})

	t.Run("no mentions leaves body unchanged", func(t *testing.T) {
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		msg, ok, err := d.Decode(mkRaw("just a normal message", `[]`), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != "just a normal message" {
			t.Errorf("Body = %q; want %q", msg.Body, "just a normal message")
		}
	})

	t.Run("preserves indentation and tabs around stripped mention", func(t *testing.T) {
		// Code-block / indented messages: stripping the bot mention
		// must not eat the surrounding indent, tabs, or any internal
		// whitespace the user intentionally typed. We only consume a
		// single space directly adjacent to the placeholder.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		mentions := `[{"key":"@_user_1","name":"My Bot","id":{"open_id":"ou_bot_wire","union_id":"on_bot"}}]`
		raw := "    @_user_1 review this snippet:\n\tfunc add(a, b int) int {\n\t\treturn a + b\n\t}"
		want := "    review this snippet:\n\tfunc add(a, b int) int {\n\t\treturn a + b\n\t}"
		msg, ok, err := d.Decode(mkRaw(raw, mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != want {
			t.Errorf("Body = %q; want %q", msg.Body, want)
		}
	})

	t.Run("avoids @_user_1 / @_user_10 prefix collision", func(t *testing.T) {
		// Lark assigns mention keys positionally; a chat with eleven+
		// participants exposes both `@_user_1` and `@_user_10`. Naive
		// ReplaceAll for `@_user_1` would mangle `@_user_10`, so we
		// match longest-first.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_bot",
			BotUnionID: pgText("on_bot"),
		}
		mentions := `[
			{"key":"@_user_1","name":"My Bot","id":{"open_id":"ou_bot_wire","union_id":"on_bot"}},
			{"key":"@_user_10","name":"Alice","id":{"open_id":"ou_alice","union_id":"on_alice"}}
		]`
		raw := "@_user_1 forward this to @_user_10 please"
		want := "forward this to @Alice please"
		msg, ok, err := d.Decode(mkRaw(raw, mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != want {
			t.Errorf("Body = %q; want %q", msg.Body, want)
		}
	})

	t.Run("@-ing both bots in one message strips only self, renders other by name", func(t *testing.T) {
		// Multi-bot group chat where the user @-mentions BOTH bots in
		// the same message. From this WS's perspective only the self
		// mention should be stripped; the sibling bot renders as
		// @<displayName> so the agent receives a faithful transcript
		// of the user intent.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_self_canonical",
			BotUnionID: pgText("on_self_union"),
		}
		mentions := `[
			{"key":"@_user_1","name":"Self Bot","id":{"open_id":"ou_self_wire","union_id":"on_self_union"}},
			{"key":"@_user_2","name":"Sibling Bot","id":{"open_id":"ou_sibling_wire","union_id":"on_sibling_union"}}
		]`
		raw := "@_user_1 @_user_2 please coordinate"
		want := "@Sibling Bot please coordinate"
		msg, ok, err := d.Decode(mkRaw(raw, mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != want {
			t.Errorf("Body = %q; want %q", msg.Body, want)
		}
	})

	t.Run("open_id match does NOT strip when union_id known but differs", func(t *testing.T) {
		// Mirror of containsMention's union_id-first rule: when we
		// know our union_id, an open_id-only match means the mention
		// is for the OTHER bot (the inverse-mapping quirk), so we
		// must render it as @<name>, not strip it.
		inst := db.LarkInstallation{
			BotOpenID:  "ou_self_canonical",
			BotUnionID: pgText("on_self_union"),
		}
		mentions := `[{"key":"@_user_1","name":"Sibling Bot","id":{"open_id":"ou_self_canonical","union_id":"on_sibling_union"}}]`
		raw := "@_user_1 hi"
		want := "@Sibling Bot hi"
		msg, ok, err := d.Decode(mkRaw(raw, mentions), inst)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if msg.Body != want {
			t.Errorf("Body = %q; want %q", msg.Body, want)
		}
	})
}

func TestLarkJSONFrameDecoderDropsHeartbeat(t *testing.T) {
	t.Parallel()
	d := NewLarkJSONFrameDecoder()
	cases := [][]byte{
		[]byte(`{"type":"heartbeat"}`),
		[]byte(`{"type":"frame_ack","data":{"id":"1"}}`),
		[]byte(`{"type":"event_callback","header":{"event_type":"im.message.unknown_kind"}}`),
	}
	for _, raw := range cases {
		msg, ok, err := d.Decode(raw, db.LarkInstallation{})
		if err != nil || ok {
			t.Errorf("Decode(%q) ok=%v err=%v; expected (false, nil)", raw, ok, err)
		}
		if msg.EventID != "" {
			t.Errorf("expected zero-value InboundMessage on drop, got %+v", msg)
		}
	}
}

func TestLarkJSONFrameDecoderEmptyRaw(t *testing.T) {
	t.Parallel()
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(nil, db.LarkInstallation{})
	if ok || err != nil {
		t.Fatalf("expected (zero, false, nil) for empty raw; got ok=%v err=%v msg=%+v", ok, err, msg)
	}
}

func TestLarkJSONFrameDecoderMalformedReturnsError(t *testing.T) {
	t.Parallel()
	_, ok, err := NewLarkJSONFrameDecoder().Decode([]byte("not-json"), db.LarkInstallation{})
	if err == nil {
		t.Fatal("expected error on malformed envelope")
	}
	if ok {
		t.Error("ok should be false on decode failure")
	}
}

func TestLarkJSONFrameDecoderMessageContentEmptyOnInvalidContentJSON(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"type":"event_callback",
		"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"}},
			"message":{"message_id":"m","chat_id":"c","chat_type":"p2p","message_type":"text","content":"not-json"}
		}
	}`)
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(raw, db.LarkInstallation{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if msg.Body != "" {
		t.Errorf("Body = %q; expected empty on unparseable content", msg.Body)
	}
}

func TestLarkJSONFrameDecoderNonTextMessageHasEmptyBody(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"type":"event_callback",
		"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"}},
			"message":{"message_id":"m","chat_id":"c","chat_type":"p2p","message_type":"image","content":"{\"image_key\":\"img1\"}"}
		}
	}`)
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(raw, db.LarkInstallation{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if msg.Body != "" {
		t.Errorf("Body = %q; non-text messages should have empty body in MVP", msg.Body)
	}
	if msg.MessageID == "" {
		t.Error("MessageID should still be populated for non-text events")
	}
}

// TestLarkJSONFrameDecoderPostMessageFlattened verifies that a rich-text
// `post` message is flattened to plain text end-to-end through Decode —
// the MUL-2951 example. Body.content is the JSON-encoded post object; we
// marshal a Go string to get the correctly-escaped content field.
func TestLarkJSONFrameDecoderPostMessageFlattened(t *testing.T) {
	t.Parallel()
	postContent := `{"title":"周报","content":[[{"tag":"text","text":"本周完成："}],[{"tag":"text","text":"Lark 集成"},{"tag":"a","href":"https://github.com/multica-ai/multica/pull/3277","text":"PR #3277"}]]}`
	escaped, err := json.Marshal(postContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := []byte(`{
		"type":"event_callback",
		"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"}},
			"message":{"message_id":"m","chat_id":"c","chat_type":"p2p","message_type":"post","content":` + string(escaped) + `}
		}
	}`)
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(raw, db.LarkInstallation{BotOpenID: "ou_bot"})
	if err != nil || !ok {
		t.Fatalf("Decode ok=%v err=%v", ok, err)
	}
	want := "周报\n本周完成：\nLark 集成 PR #3277 (https://github.com/multica-ai/multica/pull/3277)"
	if msg.Body != want {
		t.Errorf("post Body\n got = %q\nwant = %q", msg.Body, want)
	}
	if msg.MessageType != "post" {
		t.Errorf("MessageType = %q want post", msg.MessageType)
	}
}

// TestLarkJSONFrameDecoderPostResolvesMentions checks that @-mentions in
// a post (carried as `at` spans with @_user_N placeholders) are resolved
// through the same mention pipeline as text, including stripping the
// bot's own mention.
func TestLarkJSONFrameDecoderPostResolvesMentions(t *testing.T) {
	t.Parallel()
	postContent := `{"content":[[{"tag":"at","user_id":"@_user_1","user_name":""},{"tag":"text","text":"please review"},{"tag":"at","user_id":"@_user_2","user_name":""}]]}`
	escaped, err := json.Marshal(postContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := []byte(`{
		"type":"event_callback",
		"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"}},
			"message":{
				"message_id":"m","chat_id":"c","chat_type":"group","message_type":"post",
				"content":` + string(escaped) + `,
				"mentions":[
					{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"Bot"},
					{"key":"@_user_2","id":{"open_id":"ou_alice"},"name":"Alice"}
				]
			}
		}
	}`)
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(raw, db.LarkInstallation{BotOpenID: "ou_bot"})
	if err != nil || !ok {
		t.Fatalf("Decode ok=%v err=%v", ok, err)
	}
	// @_user_1 is the bot → stripped; @_user_2 → @Alice.
	want := "please review @Alice"
	if msg.Body != want {
		t.Errorf("post Body\n got = %q\nwant = %q", msg.Body, want)
	}
	if !msg.AddressedToBot {
		t.Error("AddressedToBot should be true (bot was @-mentioned)")
	}
}

// TestLarkJSONFrameDecoderCapturesReplyLinkage verifies parent_id /
// root_id from a quote-reply event land on the InboundMessage so the
// enricher can expand them.
func TestLarkJSONFrameDecoderCapturesReplyLinkage(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"type":"event_callback",
		"header":{"event_id":"e","event_type":"im.message.receive_v1","app_id":"a"},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"}},
			"message":{
				"message_id":"om_child","chat_id":"c","chat_type":"group","message_type":"text",
				"content":"{\"text\":\"去实现\"}",
				"parent_id":"om_parent","root_id":"om_root"
			}
		}
	}`)
	msg, ok, err := NewLarkJSONFrameDecoder().Decode(raw, db.LarkInstallation{BotOpenID: "ou_bot"})
	if err != nil || !ok {
		t.Fatalf("Decode ok=%v err=%v", ok, err)
	}
	if msg.ParentID != "om_parent" {
		t.Errorf("ParentID = %q want om_parent", msg.ParentID)
	}
	if msg.RootID != "om_root" {
		t.Errorf("RootID = %q want om_root", msg.RootID)
	}
	if msg.MessageType != "text" {
		t.Errorf("MessageType = %q want text", msg.MessageType)
	}
	// CommandBody snapshots the user's own text (pre-enrichment) so
	// /issue parsing survives the enricher's prepended context blocks.
	if msg.CommandBody != "去实现" {
		t.Errorf("CommandBody = %q want 去实现", msg.CommandBody)
	}
}
