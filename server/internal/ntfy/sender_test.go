package ntfy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSend_Success(t *testing.T) {
	var gotAuth, gotClick, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotClick = r.Header.Get("X-Click")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New()
	err := s.Send(context.Background(), srv.URL, "test-token", Message{
		Title:    "Test Title",
		Body:     "Test Body",
		Severity: "action_required",
		ClickURL: "https://app.example.com/issues/123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth header = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotClick != "https://app.example.com/issues/123" {
		t.Errorf("X-Click = %q, want %q", gotClick, "https://app.example.com/issues/123")
	}
	if !strings.Contains(gotBody, `"priority":5`) {
		t.Errorf("body missing priority 5: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"title":"Test Title"`) {
		t.Errorf("body missing title: %s", gotBody)
	}
}

func TestSend_NoToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New()
	err := s.Send(context.Background(), srv.URL, "", Message{Severity: "info"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestSend_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := New()
	err := s.Send(context.Background(), srv.URL, "bad-token", Message{Severity: "info"})
	if err == nil {
		t.Fatal("expected error for 4xx response")
	}
}

func TestSend_PriorityMapping(t *testing.T) {
	cases := []struct {
		severity string
		wantPri  string
	}{
		{"action_required", `"priority":5`},
		{"attention", `"priority":3`},
		{"info", `"priority":1`},
		{"unknown", `"priority":3`}, // default
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			s := New()
			_ = s.Send(context.Background(), srv.URL, "", Message{Severity: tc.severity})
			if !strings.Contains(gotBody, tc.wantPri) {
				t.Errorf("severity %q: body %q does not contain %q", tc.severity, gotBody, tc.wantPri)
			}
		})
	}
}
