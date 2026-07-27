package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/bot123:secret/getMe") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "result": map[string]any{"id": int64(123), "username": "acme_bot"},
		})
	}))
	defer srv.Close()
	c := NewClient("123:secret", WithAPIBase(srv.URL))
	info, err := c.GetMe(context.Background())
	if err != nil || info.ID != 123 || info.Username != "acme_bot" {
		t.Fatalf("got (%+v, %v)", info, err)
	}
}

func TestClientGetMeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	defer srv.Close()
	if _, err := NewClient("bad:token", WithAPIBase(srv.URL)).GetMe(context.Background()); err == nil {
		t.Fatal("expected error on ok:false")
	}
}

func TestClientSendMessageThreads(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
	}))
	defer srv.Close()
	if err := NewClient("123:s", WithAPIBase(srv.URL)).SendMessage(context.Background(), "555", "hi", "42"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if body["chat_id"] != "555" || body["text"] != "hi" || body["message_thread_id"] != "42" {
		t.Fatalf("unexpected body %+v", body)
	}
}
