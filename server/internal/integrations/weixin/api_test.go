package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQRLoginProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("QR login must not send a bot token")
		}
		if r.Header.Get("iLink-App-Id") != "bot" {
			t.Error("missing app id")
		}
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			if r.Method != http.MethodPost || r.URL.Query().Get("bot_type") != "3" {
				t.Error("wrong QR request")
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if string(body["local_token_list"]) != "[]" {
				t.Error("unexpected local credentials")
			}
			_, _ = w.Write([]byte(`{"qrcode":"opaque +/& QR","qrcode_img_content":"https://weixin.qq.com/qr/example"}`))
		case "/ilink/bot/get_qrcode_status":
			if r.Method != http.MethodGet || r.URL.Query().Get("qrcode") != "opaque +/& QR" || r.URL.Query().Get("verify_code") != "001234" {
				t.Error("lost QR/verification code encoding")
			}
			_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"test-bot-token","ilink_bot_id":"bot@im.bot","ilink_user_id":"owner@im.wechat","baseurl":"https://ilinkai.weixin.qq.com"}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	api := newAPI(server.URL, "must-not-be-sent", server.Client())
	qr, err := api.QRCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, err := api.QRStatus(context.Background(), qr.QRCode, "001234")
	if err != nil || status.UserID != "owner@im.wechat" {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestQRVerificationStates(t *testing.T) {
	for _, status := range []string{"need_verifycode", "confirmed_but_need_verifycode", "future_need_verifycode"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
			}))
			defer server.Close()
			result, err := newAPI(server.URL, "", server.Client()).QRStatus(context.Background(), "qr", "")
			if err != nil || result.Status != status {
				t.Fatalf("verification status rejected: %q, %v", result.Status, err)
			}
		})
	}
}

func TestUpdatesPreserveMessageIDAndReplyContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("AuthorizationType") != "ilink_bot_token" {
			t.Error("missing authentication")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body["base_info"]) == 0 {
			t.Error("missing base_info")
		}
		if strings.HasSuffix(r.URL.Path, "getupdates") {
			if string(body["get_updates_buf"]) != `"cursor-1"` {
				t.Error("cursor not resumed")
			}
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-2","msgs":[{"message_id":18446744073709551615,"from_user_id":"owner","message_type":1,"context_token":"context","item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`))
		} else {
			var msg message
			if err := json.Unmarshal(body["msg"], &msg); err != nil {
				t.Fatal(err)
			}
			if msg.ContextToken != "context" || msg.To != "owner" || msg.Type != 2 || msg.State == nil || *msg.State != 2 || msg.ClientID != "stable-send" {
				t.Error("incorrect reply addressing")
			}
			_, _ = w.Write([]byte(`{"ret":0,"message_id":"18446744073709551614"}`))
		}
	}))
	defer server.Close()
	api := newAPI(server.URL, "test-token", server.Client())
	result, err := api.Updates(context.Background(), "cursor-1")
	if err != nil || len(result.Messages) != 1 || result.Messages[0].ID != "18446744073709551615" {
		t.Fatalf("updates = %#v, %v", result, err)
	}
	msg := result.Messages[0]
	got, err := api.Send(context.Background(), textMessage(msg.From, msg.ContextToken, "stable-send", "reply"))
	if err != nil || got != "18446744073709551614" {
		t.Fatalf("send = %q, %v", got, err)
	}
}

func TestProviderFailuresAndMissingContext(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"expired", `{"ret":-14}`, ErrTokenExpired},
		{"expired errcode", `{"errcode":-14}`, ErrTokenExpired},
		{"missing status", `{}`, ErrProtocol},
		{"invalid json", `{"bot_token":"secret"`, ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			_, err := newAPI(server.URL, "token", server.Client()).Updates(context.Background(), "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	_, err := newAPI("", "", nil).Send(context.Background(), textMessage("owner", "", "id", "text"))
	if !errors.Is(err, ErrNoContext) {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdatesAcceptSuccessWithoutRet(t *testing.T) {
	for _, body := range []string{
		`{"msgs":[],"sync_buf":"sync","get_updates_buf":"next"}`,
		`{"msgs":[{"message_id":123,"from_user_id":"owner","message_type":1,"item_list":[{"type":1,"text_item":{"text":"hello"}}]}],"get_updates_buf":"next"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			result, err := newAPI(server.URL, "token", server.Client()).Updates(context.Background(), "")
			if err != nil || result.Cursor != "next" {
				t.Fatalf("successful update rejected: cursor=%q error=%v", result.Cursor, err)
			}
		})
	}
}

func TestSendAcceptSuccessWithoutRet(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"message_id":"provider-id"}`, "provider-id"},
		{`{}`, "client-id"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := newAPI(server.URL, "token", server.Client()).Send(context.Background(), textMessage("owner", "context", "client-id", "reply"))
			if err != nil || got != tc.want {
				t.Fatalf("successful send rejected: id=%q error=%v", got, err)
			}
		})
	}
}

func TestSendRejectsProviderErrors(t *testing.T) {
	for _, body := range []string{`{"ret":-14}`, `{"errcode":-14}`, `{"ret":123}`, `{"errcode":456}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			got, err := newAPI(server.URL, "token", server.Client()).Send(context.Background(), textMessage("owner", "context", "client-id", "reply"))
			if err == nil || got != "" {
				t.Fatalf("provider failure accepted: id=%q error=%v", got, err)
			}
		})
	}
}

func TestNotifyOptionalReturnCode(t *testing.T) {
	for _, start := range []bool{true, false} {
		for _, body := range []string{`{}`, `{"ret":0}`, `{"errcode":0}`, `{"ret":123}`, `{"errcode":456}`} {
			t.Run(body, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
				defer server.Close()
				err := newAPI(server.URL, "token", server.Client()).notify(context.Background(), start)
				wantError := strings.Contains(body, "123") || strings.Contains(body, "456")
				if (err != nil) != wantError {
					t.Fatalf("notify error=%v, wantError=%v", err, wantError)
				}
			})
		}
	}
}

func TestProviderURLsAndRedirects(t *testing.T) {
	for _, raw := range []string{"http://ilinkai.weixin.qq.com", "https://weixin.qq.com.evil.test", "https://evil.test/", "https://user:pass@ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com:8443", "https://ilinkai.weixin.qq.com/path"} {
		if _, err := validateBaseURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := validateBaseURL("https://ilinkai.weixin.qq.com/"); err != nil {
		t.Fatal(err)
	}
	visited := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visited = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := newAPI(server.URL, "secret", server.Client()).Updates(context.Background(), "")
	if err == nil || visited {
		t.Fatal("followed redirect with a credential")
	}
}
