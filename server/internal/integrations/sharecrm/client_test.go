package sharecrm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAccessTokenAndSend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/im-gateway/auth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"accessToken": "tok-1",
				"expiresIn":   7200,
				"tokenType":   "Bearer",
			},
		})
	})
	mux.HandleFunc("/im-gateway/qixin/message/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["chat_id"] != "0:fs:s:" || body["text"] != "hi" {
			t.Errorf("unexpected body %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message_id": "out-9"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	tok, exp, err := FetchAccessToken(ctx, srv.Client(), srv.URL, "app", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-1" || exp != 7200 {
		t.Fatalf("token=%q exp=%d", tok, exp)
	}

	c := NewClient(srv.Client())
	msgID, err := c.SendMessage(ctx, srv.URL, "app", "secret", "0:fs:s:", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "out-9" {
		t.Fatalf("msgID=%q", msgID)
	}
}

func TestSendMessage_BotNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/im-gateway/auth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"accessToken": "t", "expiresIn": 3600, "tokenType": "Bearer"},
		})
	})
	mux.HandleFunc("/im-gateway/qixin/message/send", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": codeBotNotConnected, "msg": "Bot not connected"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.Client())
	_, err := c.SendMessage(context.Background(), srv.URL, "a", "s", "0:fs:x:", "hi", nil)
	if err == nil || err.Error() == "" {
		t.Fatal("expected error")
	}
}
