package forgejo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeInstanceURL(t *testing.T) {
	cases := map[string]string{
		"https://forgejo.example.com/": "https://forgejo.example.com",
		"  https://forge.test  ":       "https://forge.test",
		"https://forge.test/sub/":      "https://forge.test/sub",
		"http://localhost:3000":        "http://localhost:3000",
	}
	for in, want := range cases {
		if got := NormalizeInstanceURL(in); got != want {
			t.Errorf("NormalizeInstanceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	valid := hex.EncodeToString(mac.Sum(nil))

	if !VerifyWebhookSignature(secret, valid, body) {
		t.Error("valid bare hex signature rejected")
	}
	if !VerifyWebhookSignature(secret, "sha256="+valid, body) {
		t.Error("valid sha256-prefixed signature rejected")
	}
	if VerifyWebhookSignature(secret, valid, []byte("tampered")) {
		t.Error("signature accepted for tampered body")
	}
	if VerifyWebhookSignature("wrongsecret", valid, body) {
		t.Error("signature accepted under wrong secret")
	}
	if VerifyWebhookSignature(secret, "not-hex", body) {
		t.Error("non-hex signature accepted")
	}
	if VerifyWebhookSignature(secret, "", body) {
		t.Error("empty signature accepted")
	}
}

func TestCurrentUser(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/user" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "token abc123" {
				t.Errorf("Authorization = %q, want %q", got, "token abc123")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"octo","id":1}`))
		}))
		defer srv.Close()

		u, err := NewClient(srv.URL, "abc123").CurrentUser(context.Background())
		if err != nil {
			t.Fatalf("CurrentUser: %v", err)
		}
		if u.Login != "octo" {
			t.Errorf("login = %q, want octo", u.Login)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "bad").CurrentUser(context.Background())
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "x").CurrentUser(context.Background())
		if err == nil || errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want generic non-unauthorized error", err)
		}
	})

	t.Run("missing login", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"id":1}`))
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "x").CurrentUser(context.Background())
		if err == nil {
			t.Fatal("expected error for response missing login")
		}
	})
}
