package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), ClientOptions{
		BaseURL:        server.URL,
		ILinkAppID:     "bot",
		ClientVersion:  "132102",
		ChannelVersion: "test-version",
		BotAgent:       "Multica/Test",
		AllowBaseURL:   func(*url.URL) bool { return true },
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func TestBeginQRCodeUsesOfficialRequestShape(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/"+endpointGetQRCode {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("bot_type"); got != defaultBotType {
			t.Errorf("bot_type = %q", got)
		}
		assertCommonHeaders(t, r, false)
		var body struct {
			LocalTokens []string `json:"local_token_list"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(body.LocalTokens) != 1 || body.LocalTokens[0] != "old-token" {
			t.Errorf("local_token_list = %#v", body.LocalTokens)
		}
		_ = json.NewEncoder(w).Encode(QRCode{Code: "opaque-code", ImageContent: "https://example.invalid/qr"})
	})

	got, err := client.BeginQRCode(context.Background(), []string{"old-token"})
	if err != nil {
		t.Fatalf("BeginQRCode: %v", err)
	}
	if got.Code != "opaque-code" || got.ImageContent != "https://example.invalid/qr" {
		t.Fatalf("QRCode = %#v", got)
	}
}

func TestPollQRCodeEscapesOpaqueValuesAndValidatesConfirmation(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Query().Get("qrcode") != "opaque +/?" {
			t.Errorf("qrcode = %q", r.URL.Query().Get("qrcode"))
		}
		if r.URL.Query().Get("verify_code") != "123 456" {
			t.Errorf("verify_code = %q", r.URL.Query().Get("verify_code"))
		}
		assertCommonHeaders(t, r, true)
		_ = json.NewEncoder(w).Encode(QRStatus{
			Status:    "confirmed",
			BotToken:  "secret",
			BotID:     "bot@im.bot",
			BaseURL:   defaultBaseURL,
			ScannerID: "user@im.wechat",
		})
	})

	got, err := client.PollQRCode(context.Background(), server.URL, "opaque +/?", "123 456")
	if err != nil {
		t.Fatalf("PollQRCode: %v", err)
	}
	if !got.confirmed() || got.BotID != "bot@im.bot" || got.ScannerID != "user@im.wechat" {
		t.Fatalf("QRStatus = %#v", got)
	}
}

func TestPollQRCodeRejectsIncompleteConfirmation(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(QRStatus{Status: "confirmed", BotID: "bot@im.bot"})
	})
	_, err := client.PollQRCode(context.Background(), server.URL, "code", "")
	if err == nil || !strings.Contains(err.Error(), "bot_token") || !strings.Contains(err.Error(), "baseurl") {
		t.Fatalf("error = %v", err)
	}
}

func TestPollQRCodeTreatsInternalLongPollTimeoutAsWait(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"wait"}`))
	})
	client.pollTimeout = 10 * time.Millisecond
	got, err := client.PollQRCode(context.Background(), server.URL, "code", "")
	if err != nil {
		t.Fatalf("PollQRCode: %v", err)
	}
	if got.Status != "wait" {
		t.Fatalf("status = %#v", got)
	}
}

func TestGetUpdatesAuthenticatesAndPreservesProtocolFields(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+endpointGetUpdates {
			t.Errorf("path = %q", r.URL.Path)
		}
		assertCommonHeaders(t, r, false)
		if got := r.Header.Get("Authorization"); got != "Bearer token-value" {
			t.Errorf("Authorization = %q", got)
		}
		var body getUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Cursor != "cursor-1" {
			t.Errorf("cursor = %q", body.Cursor)
		}
		if body.BaseInfo.ChannelVersion != "test-version" || body.BaseInfo.BotAgent != "Multica/Test" {
			t.Errorf("base_info = %#v", body.BaseInfo)
		}
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-2","longpolling_timeout_ms":41000,"msgs":[{"message_id":42,"from_user_id":"u@im.wechat","to_user_id":"b@im.bot","message_type":1,"item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`))
	})

	got, err := client.GetUpdates(context.Background(), server.URL, "token-value", "cursor-1", time.Second)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if got.Cursor != "cursor-2" || got.LongPollMillis != 41000 || len(got.Messages) != 1 {
		t.Fatalf("Updates = %#v", got)
	}
	if id := rawMessageID(got.Messages[0].MessageID); id != "42" {
		t.Fatalf("message id = %q", id)
	}
}

func TestGetUpdatesMapsStaleToken(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14,"errcode":-14,"errmsg":"session timeout"}`))
	})
	_, err := client.GetUpdates(context.Background(), server.URL, "token", "", time.Second)
	if !errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("error = %v, want ErrReauthorizationRequired", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Operation != "getupdates" {
		t.Fatalf("error = %#v, want APIError", err)
	}
}

func TestGetUpdatesTreatsInternalLongPollTimeoutAsEmpty(t *testing.T) {
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ret":0}`))
	})
	got, err := client.GetUpdates(context.Background(), server.URL, "token", "cursor-1", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if got.Cursor != "cursor-1" || len(got.Messages) != 0 {
		t.Fatalf("Updates = %#v", got)
	}
}

func TestSendTextUsesStableClientIDAndChecksRet(t *testing.T) {
	requests := 0
	client, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body sendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		msg := body.Message
		if msg.ClientID != "stable-client-id" || msg.ToUserID != "u@im.wechat" || msg.ContextToken != "context-secret" {
			t.Errorf("message = %#v", msg)
		}
		if msg.MessageType != messageTypeBot || msg.MessageState != messageStateFinish || len(msg.Items) != 1 || msg.Items[0].Text.Text != "reply" {
			t.Errorf("message payload = %#v", msg)
		}
		if requests == 1 {
			_, _ = w.Write([]byte(`{"ret":0}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":-2,"errmsg":"bad context"}`))
	})

	in := SendTextInput{
		BaseURL:      server.URL,
		Token:        "token",
		ToUserID:     "u@im.wechat",
		ContextToken: "context-secret",
		Text:         "reply",
		ClientID:     "stable-client-id",
	}
	got, err := client.SendText(context.Background(), in)
	if err != nil || got != "stable-client-id" {
		t.Fatalf("SendText = (%q, %v)", got, err)
	}
	got, err = client.SendText(context.Background(), in)
	if got != "stable-client-id" {
		t.Fatalf("client id on failure = %q", got)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Ret != -2 {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientRejectsUntrustedBaseURL(t *testing.T) {
	client, err := NewClient(nil, ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.GetUpdates(context.Background(), "https://attacker.example", "token", "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error = %v", err)
	}
	if _, err := client.RedirectBaseURL("evil.weixin.qq.com@attacker.example"); err == nil {
		t.Fatal("RedirectBaseURL accepted userinfo-like host")
	}
}

func TestChunkTextPreservesRuneBoundaries(t *testing.T) {
	got := ChunkText("甲乙丙丁戊", 2)
	want := []string{"甲乙", "丙丁", "戊"}
	if len(got) != len(want) {
		t.Fatalf("chunks = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunks = %#v", got)
		}
	}
}

func assertCommonHeaders(t *testing.T, r *http.Request, isGET bool) {
	t.Helper()
	if got := r.Header.Get("iLink-App-Id"); got != "bot" {
		t.Errorf("iLink-App-Id = %q", got)
	}
	if got := r.Header.Get("iLink-App-ClientVersion"); got != "132102" {
		t.Errorf("iLink-App-ClientVersion = %q", got)
	}
	if isGET {
		if got := r.Header.Get("AuthorizationType"); got != "" {
			t.Errorf("GET AuthorizationType = %q", got)
		}
		return
	}
	if got := r.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
		t.Errorf("AuthorizationType = %q", got)
	}
	uin := r.Header.Get("X-WECHAT-UIN")
	decoded, err := base64.StdEncoding.DecodeString(uin)
	if err != nil || len(decoded) == 0 {
		t.Errorf("X-WECHAT-UIN = %q, err=%v", uin, err)
	}
}
