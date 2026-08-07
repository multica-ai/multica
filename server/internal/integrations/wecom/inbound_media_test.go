package wecom

// inbound_media_test.go — what a media callback becomes on the way in:
// whether it reaches the handler at all, what the stored body says, and what
// the resolver is handed to download.
//
// The frames here are the shapes WeCom's aibot long-connection docs specify
// (developer.work.weixin.qq.com/document/path/101463): a standalone
// image/file/video carries {url, aeskey} under its own key, and a 图文混排
// carries mixed.msg_item, each item typed like a standalone message.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// mediaFrame builds an aibot_msg_callback frame from a raw body map, going
// through the real JSON decoder so a test cannot drift from the wire.
func mediaFrame(t *testing.T, msgType string, extra map[string]any) frameEnvelope {
	t.Helper()
	full := map[string]any{
		"msgid":    "MSGID-1",
		"aibotid":  "bot-1",
		"chatid":   "CHAT_1",
		"chattype": "single",
		"from":     map[string]any{"userid": "USER_A"},
		"msgtype":  msgType,
	}
	for k, v := range extra {
		full[k] = v
	}
	body, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return frameEnvelope{Cmd: cmdMsgCallback, Body: body}
}

// dispatchOne runs one frame through dispatchFrame and reports what the
// handler saw plus what went back over the socket.
func dispatchOne(t *testing.T, env frameEnvelope) (channel.InboundMessage, bool, *recordingConn) {
	t.Helper()
	var got channel.InboundMessage
	called := false
	c := testChannel(func(_ context.Context, m channel.InboundMessage) error {
		called, got = true, m
		return nil
	})
	conn := &recordingConn{}
	if err := c.dispatchFrame(context.Background(), env, newWSSender(conn, slog.Default()), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	return got, called, conn
}

// TestDispatchFrame_MediaCallbacksReachTheHandler is the defect this file
// exists for. Before inbound media, dispatchFrame routed only msgtype ==
// "text" and answered every other kind with a receipt, so a photo, a file, a
// video and a 图文混排 all ended at the socket: no chat_message, no session,
// no agent run, nothing in the web UI either.
func TestDispatchFrame_MediaCallbacksReachTheHandler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		msgType  string
		extra    map[string]any
		wantText string
		wantType channel.MsgType
		wantURLs []string
	}{
		{
			name:     "image",
			msgType:  "image",
			extra:    map[string]any{"image": map[string]any{"url": "https://cos.example.com/i", "aeskey": "k1"}},
			wantText: "[Image]",
			wantType: channel.MsgTypeImage,
			wantURLs: []string{"https://cos.example.com/i"},
		},
		{
			name:     "file",
			msgType:  "file",
			extra:    map[string]any{"file": map[string]any{"url": "https://cos.example.com/f", "aeskey": "k2"}},
			wantText: "[File]",
			wantType: channel.MsgTypeFile,
			wantURLs: []string{"https://cos.example.com/f"},
		},
		{
			name:     "video",
			msgType:  "video",
			extra:    map[string]any{"video": map[string]any{"url": "https://cos.example.com/v", "aeskey": "k3"}},
			wantText: "[Video]",
			wantType: channel.MsgTypeVideo,
			wantURLs: []string{"https://cos.example.com/v"},
		},
		{
			name:    "mixed keeps composition order",
			msgType: "mixed",
			extra: map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
				{"msgtype": "text", "text": map[string]any{"content": "看下这个报错"}},
				{"msgtype": "image", "image": map[string]any{"url": "https://cos.example.com/m1", "aeskey": "k4"}},
				{"msgtype": "text", "text": map[string]any{"content": "还有这个"}},
				{"msgtype": "image", "image": map[string]any{"url": "https://cos.example.com/m2", "aeskey": "k5"}},
			}}},
			wantText: "看下这个报错\n[Image]\n还有这个\n[Image]",
			wantType: channel.MsgTypeText,
			wantURLs: []string{"https://cos.example.com/m1", "https://cos.example.com/m2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, called, conn := dispatchOne(t, mediaFrame(t, tc.msgType, tc.extra))
			if !called {
				t.Fatalf("a %s message never reached the handler — it was answered and dropped", tc.msgType)
			}
			if len(conn.frames) != 0 {
				t.Errorf("a routable %s message should not also get a receipt, got %d frames", tc.msgType, len(conn.frames))
			}
			if got.Text != tc.wantText {
				t.Errorf("stored body = %q, want %q", got.Text, tc.wantText)
			}
			if got.Type != tc.wantType {
				t.Errorf("normalized type = %v, want %v", got.Type, tc.wantType)
			}
			var wm InboundMessage
			if err := json.Unmarshal(got.Raw, &wm); err != nil {
				t.Fatalf("decode Raw: %v", err)
			}
			if len(wm.Media) != len(tc.wantURLs) {
				t.Fatalf("Raw carried %d attachments, want %d", len(wm.Media), len(tc.wantURLs))
			}
			for i, want := range tc.wantURLs {
				if wm.Media[i].URL != want {
					t.Errorf("attachment %d url = %q, want %q (order must be composition order)", i, wm.Media[i].URL, want)
				}
			}
		})
	}
}

// TestMediaPlaceholders_MatchLarkAndDingTalk pins the placeholder strings to
// their siblings byte for byte. An agent reads every channel through the same
// prompt, so a wecom-only spelling is a wecom-only thing for it to learn.
//
// Sources: lark/content_flatten.go flattenContent ("[Image]" / "[File]" /
// "[Video]") and dingtalk/inbound.go dingtalkImagePlaceholder = "[Image]"
// with "[File]" beside it.
func TestMediaPlaceholders_MatchLarkAndDingTalk(t *testing.T) {
	t.Parallel()
	cases := map[channel.MsgType]string{
		channel.MsgTypeImage: "[Image]",
		channel.MsgTypeFile:  "[File]",
		channel.MsgTypeVideo: "[Video]",
	}
	for kind, want := range cases {
		if got := mediaPlaceholder(kind); got != want {
			t.Errorf("mediaPlaceholder(%v) = %q, want %q — lark and dingtalk emit %q", kind, got, want, want)
		}
	}
}

// TestOwnText_MixedDropsRunsItCannotRead: a run of a kind the adapter does
// not model contributes nothing, rather than a stray placeholder for an
// attachment nobody will ever download.
func TestOwnText_MixedDropsRunsItCannotRead(t *testing.T) {
	t.Parallel()
	env := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "text", "text": map[string]any{"content": "hi"}},
		{"msgtype": "location", "location": map[string]any{"name": "office"}},
		{"msgtype": "image", "image": map[string]any{"url": "", "aeskey": "k"}},
	}}})
	got, called, _ := dispatchOne(t, env)
	if !called {
		t.Fatal("a mixed message with one readable run must still be routed")
	}
	if got.Text != "hi" {
		t.Errorf("body = %q, want just the readable run", got.Text)
	}
	var wm InboundMessage
	if err := json.Unmarshal(got.Raw, &wm); err != nil {
		t.Fatalf("decode Raw: %v", err)
	}
	if len(wm.Media) != 0 {
		t.Errorf("an image run with no url produced %d attachments, want 0 — an intent row for an object that can never exist", len(wm.Media))
	}
}

// TestOwnText_MixedWithNothingReadableTakesTheReceipt: a mixed message whose
// every run is a kind we cannot read is not an empty message to ingest.
func TestOwnText_MixedWithNothingReadableTakesTheReceipt(t *testing.T) {
	t.Parallel()
	env := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "location", "location": map[string]any{"name": "office"}},
	}}})
	_, called, conn := dispatchOne(t, env)
	if called {
		t.Error("a mixed message with nothing readable must not be ingested as an empty turn")
	}
	if len(conn.frames) != 1 {
		t.Fatalf("expected one receipt, got %d frames", len(conn.frames))
	}
}

// TestUnsupportedReceipt_DoesNotClaimTextOnly: the receipt used to say the
// bot only handles text. It now routes photos, files, videos and 图文混排, so
// a person who just watched it answer a screenshot must not then be told it
// handles text only.
func TestUnsupportedReceipt_DoesNotClaimTextOnly(t *testing.T) {
	t.Parallel()
	if strings.Contains(unsupportedMsgTypeReceipt, "只能处理文字") {
		t.Errorf("receipt %q still claims text-only while image/file/video/mixed route", unsupportedMsgTypeReceipt)
	}
}

// TestVoiceStaysOnTheReceiptPath: a STANDALONE voice note is #6599's subject,
// not this change's. It must still take the receipt path here, so the two
// land independently. A voice RUN inside a 图文混排 is this change's, because
// dropping it would lose a spoken sentence out of a message whose other runs
// are read.
func TestVoiceStaysOnTheReceiptPath(t *testing.T) {
	t.Parallel()
	standalone := mediaFrame(t, "voice", map[string]any{"voice": map[string]any{"content": "把报表发我"}})
	if _, called, conn := dispatchOne(t, standalone); called || len(conn.frames) != 1 {
		t.Errorf("standalone voice should still take the receipt path (called=%v, frames=%d)", called, len(conn.frames))
	}

	inMixed := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "voice", "voice": map[string]any{"content": "把报表发我"}},
		{"msgtype": "file", "file": map[string]any{"url": "https://cos.example.com/x", "aeskey": "k"}},
	}}})
	got, called, _ := dispatchOne(t, inMixed)
	if !called {
		t.Fatal("a mixed message with a voice run must be routed")
	}
	if got.Text != "把报表发我\n[File]" {
		t.Errorf("body = %q, want the transcript above the file placeholder", got.Text)
	}
}
