package speech

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDeepgramNewSession(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/grant" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"access_token":"grant-token","expires_in":30}`))
	}))
	defer server.Close()

	d := NewDeepgram(DeepgramConfig{APIKey: "key", APIURL: server.URL + "/"})
	session, err := d.NewSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token key" || gotBody["ttl_seconds"] != float64(30) {
		t.Fatalf("grant request: auth=%q body=%v", gotAuth, gotBody)
	}
	if session.Provider != "deepgram" || session.Token != "grant-token" || session.ExpiresIn != 30 {
		t.Fatalf("session: %+v", session)
	}
	if strings.Contains(session.URL, "key") {
		t.Fatal("the stream URL must never carry the API key")
	}
	u, err := url.Parse(session.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "wss" || u.Host != "api.deepgram.com" || u.Path != "/v2/listen" {
		t.Fatalf("stream URL: %s", session.URL)
	}
	q := u.Query()
	if q.Get("model") != "flux-general-en" || q.Get("encoding") != "linear16" || q.Get("sample_rate") != "16000" {
		t.Fatalf("stream query: %v", q)
	}
}

func TestDeepgramNewSessionUsesTheConfiguredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":30}`))
	}))
	defer server.Close()
	session, err := NewDeepgram(DeepgramConfig{APIKey: "key", APIURL: server.URL, Model: "flux-custom"}).NewSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(session.URL, "model=flux-custom") {
		t.Fatalf("stream URL: %s", session.URL)
	}
}

func TestDeepgramNewSessionErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"refused": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"err_msg":"bad key"}`, http.StatusUnauthorized)
		},
		"no token": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
		"not json": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
	} {
		server := httptest.NewServer(handler)
		_, err := NewDeepgram(DeepgramConfig{APIKey: "key", APIURL: server.URL}).NewSession(context.Background())
		server.Close()
		if err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("DEEPGRAM_API_KEY", "")
	if FromEnv() != nil {
		t.Fatal("no key must mean no issuer")
	}
	t.Setenv("DEEPGRAM_API_KEY", " key ")
	if FromEnv() == nil {
		t.Fatal("a key must turn voice input on")
	}
}
