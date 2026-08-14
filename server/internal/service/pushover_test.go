package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	testPushoverApplicationToken = "abcdefghijklmnopqrstuvwxyz1234"
	testPushoverUserKey          = "ZYXWVUTSRQPONMLKJIHGFEDCBA4321"
)

func TestIsValidPushoverKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		want bool
	}{
		{name: "valid", key: testPushoverUserKey, want: true},
		{name: "trimmed", key: "  " + testPushoverUserKey + "  ", want: true},
		{name: "short", key: "abc", want: false},
		{name: "punctuation", key: "abcdefghijklmnopqrstuvwxyz12-_", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidPushoverKey(tc.key); got != tc.want {
				t.Fatalf("IsValidPushoverKey() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPushoverServiceSendLoginCode(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		received = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	svc := newPushoverService(testPushoverApplicationToken, server.URL, server.Client())
	if err := svc.SendLoginCode(context.Background(), testPushoverUserKey, "123456"); err != nil {
		t.Fatalf("SendLoginCode: %v", err)
	}

	want := map[string]string{
		"token":   testPushoverApplicationToken,
		"user":    testPushoverUserKey,
		"title":   "Multica Login Code",
		"message": "123456",
	}
	for key, value := range want {
		if got := received.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestPushoverServiceSendTestNotification(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		received = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	svc := newPushoverService(testPushoverApplicationToken, server.URL, server.Client())
	if err := svc.SendTestNotification(context.Background(), testPushoverUserKey); err != nil {
		t.Fatalf("SendTestNotification: %v", err)
	}

	want := map[string]string{
		"token":   testPushoverApplicationToken,
		"user":    testPushoverUserKey,
		"title":   "Multica Test Notification",
		"message": "You are now setup to receive Pushover notifications via Multica.",
	}
	for key, value := range want {
		if got := received.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestPushoverServiceReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":0,"request":"request-id","errors":["user identifier is invalid"]}`))
	}))
	defer server.Close()

	svc := newPushoverService(testPushoverApplicationToken, server.URL, server.Client())
	err := svc.SendLoginCode(context.Background(), testPushoverUserKey, "123456")
	if err == nil || !strings.Contains(err.Error(), "user identifier is invalid") {
		t.Fatalf("SendLoginCode error = %v", err)
	}
}
