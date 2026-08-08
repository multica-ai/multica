package weixin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClientGetQRCodeUsesPersonalWeixinEndpoint(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "ilinkai.weixin.qq.com" || req.URL.Path != "/ilink/bot/get_bot_qrcode" || req.URL.Query().Get("bot_type") != "3" {
			t.Fatalf("unexpected QR URL: %s", req.URL)
		}
		if req.Header.Get("iLink-App-Id") != "bot" {
			t.Fatalf("missing iLink app header")
		}
		return jsonResponse(`{"qrcode":"code-1","qrcode_img_content":"weixin://scan/value"}`), nil
	})})
	qr, err := client.GetQRCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if qr.Code != "code-1" || qr.Content != "weixin://scan/value" {
		t.Fatalf("unexpected QR response: %+v", qr)
	}
}

func TestClientSendTextCarriesContextToken(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected send URL: %s", req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer secret" || req.Header.Get("AuthorizationType") != "ilink_bot_token" {
			t.Fatalf("unexpected auth headers: %#v", req.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		msg := body["msg"].(map[string]any)
		if msg["to_user_id"] != "owner" || msg["context_token"] != "ctx-1" {
			t.Fatalf("unexpected message: %#v", msg)
		}
		base := body["base_info"].(map[string]any)
		if base["channel_version"] != channelVersion {
			t.Fatalf("unexpected base_info: %#v", base)
		}
		return jsonResponse(`{"ret":0,"errcode":0}`), nil
	})})
	if _, err := client.SendText(context.Background(), DefaultBaseURL, "secret", "owner", "hello", "ctx-1"); err != nil {
		t.Fatal(err)
	}
}

func TestLiveSenderRetriesExpiredContextWithoutToken(t *testing.T) {
	calls := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		msg := body["msg"].(map[string]any)
		if calls == 1 {
			if msg["context_token"] != "stale" {
				t.Fatalf("first call omitted context token: %#v", msg)
			}
			return jsonResponse(`{"ret":-14,"errcode":-14,"errmsg":"expired"}`), nil
		}
		if _, exists := msg["context_token"]; exists {
			t.Fatalf("retry retained expired context token: %#v", msg)
		}
		return jsonResponse(`{"ret":0,"errcode":0}`), nil
	})})
	sender := newLiveSender(client, Credentials{BaseURL: DefaultBaseURL, Token: "secret"})
	sender.setContext("owner", "stale")
	if _, err := sender.send(context.Background(), "owner", "hello"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || sender.context("owner") != "" {
		t.Fatalf("calls=%d context=%q, want 2 and empty", calls, sender.context("owner"))
	}
}

func TestChannelAcceptsOnlyScanningAccountDirectMessages(t *testing.T) {
	var received []channel.InboundMessage
	ch := &weixinChannel{
		id:    pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		creds: Credentials{BotID: "bot-1", WeixinUserID: "owner"},
		handler: func(_ context.Context, msg channel.InboundMessage) error {
			received = append(received, msg)
			return nil
		},
	}
	sender := newLiveSender(NewClient(nil), ch.creds)
	textItem := []MessageItem{{Type: 1, TextItem: &TextItem{Text: "turn on the lights"}}}

	ch.handle(context.Background(), sender, InboundMessage{MessageID: json.RawMessage(`"m-other"`), FromUserID: "other", ItemList: textItem})
	ch.handle(context.Background(), sender, InboundMessage{MessageID: json.RawMessage(`"m-group"`), FromUserID: "owner", RoomID: "room", ItemList: textItem})
	ch.handle(context.Background(), sender, InboundMessage{MessageID: json.RawMessage(`"m-owner"`), FromUserID: "owner", ContextToken: "ctx", ItemList: textItem})

	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1", len(received))
	}
	if received[0].Source.ChatType != channel.ChatTypeP2P || received[0].Text != "turn on the lights" {
		t.Fatalf("unexpected normalized message: %+v", received[0])
	}
	if sender.context("owner") != "ctx" {
		t.Fatalf("context token was not retained")
	}
}

func TestNormalizeBaseURLRejectsUntrustedHosts(t *testing.T) {
	for _, raw := range []string{"http://ilinkai.weixin.qq.com", "https://example.com", "https://weixin.qq.com.evil.test"} {
		if _, err := normalizeBaseURL(raw); err == nil {
			t.Fatalf("normalizeBaseURL(%q) accepted an untrusted URL", raw)
		}
	}
}
