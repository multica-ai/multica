package ntfy

import (
"context"
"io"
"net/http"
"net/http/httptest"
"testing"
)

func TestSend_Success(t *testing.T) {
var gotAuth, gotClick, gotTitle, gotPriority, gotBody string
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
gotAuth = r.Header.Get("Authorization")
gotClick = r.Header.Get("X-Click")
gotTitle = r.Header.Get("X-Title")
gotPriority = r.Header.Get("X-Priority")
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
if gotTitle != "Test Title" {
t.Errorf("X-Title = %q, want %q", gotTitle, "Test Title")
}
if gotPriority != "5" {
t.Errorf("X-Priority = %q, want %q", gotPriority, "5")
}
if gotBody != "Test Body" {
t.Errorf("body = %q, want %q", gotBody, "Test Body")
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
{"action_required", "5"},
{"attention", "3"},
{"info", "1"},
{"unknown", "3"}, // default
}
for _, tc := range cases {
t.Run(tc.severity, func(t *testing.T) {
var gotPriority string
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
gotPriority = r.Header.Get("X-Priority")
w.WriteHeader(http.StatusOK)
}))
defer srv.Close()

s := New()
_ = s.Send(context.Background(), srv.URL, "", Message{Severity: tc.severity})
if gotPriority != tc.wantPri {
t.Errorf("severity %q: X-Priority = %q, want %q", tc.severity, gotPriority, tc.wantPri)
}
})
}
}

func TestSend_EmptyBodyFallsBackToTitle(t *testing.T) {
var gotBody string
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
b, _ := io.ReadAll(r.Body)
gotBody = string(b)
w.WriteHeader(http.StatusOK)
}))
defer srv.Close()

s := New()
_ = s.Send(context.Background(), srv.URL, "", Message{
Title:    "Fallback Title",
Body:     "",
Severity: "info",
})
if gotBody != "Fallback Title" {
t.Errorf("body = %q, want fallback to title %q", gotBody, "Fallback Title")
}
}
