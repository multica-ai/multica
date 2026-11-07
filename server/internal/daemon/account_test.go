package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrackAccountResolvesTheTokenUser(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		calls++
		_, _ = w.Write([]byte(`{"id":"user-1","name":"Someone"}`))
	}))
	defer srv.Close()
	client := NewClient(srv.URL)
	client.SetToken("token")
	d := &Daemon{client: client, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if d.accountUserID() != "" {
		t.Fatal("account known before resolving")
	}
	d.trackAccount(context.Background())
	if d.accountUserID() != "user-1" || calls != 1 {
		t.Fatalf("account=%q calls=%d", d.accountUserID(), calls)
	}
}

func TestTrackAccountStaysUnknownOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d.trackAccount(ctx)
	if d.accountUserID() != "" {
		t.Fatal("a failed lookup produced an account")
	}
}
