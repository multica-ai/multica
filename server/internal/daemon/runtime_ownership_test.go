package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPreflightAuthRuntimeOwnership(t *testing.T) {
	redirectTestHome(t, t.TempDir())

	var workspaceSyncs atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			_, _ = io.WriteString(w, `{}`)
		case "/api/me":
			if r.Header.Get("Authorization") == "Bearer other-account" {
				_, _ = io.WriteString(w, `{"id":"other"}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"owner"}`)
			}
		case "/api/daemon/workspaces":
			workspaceSyncs.Add(1)
			_, _ = io.WriteString(w, `[]`)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	otherServer := httptest.NewServer(handler)
	defer otherServer.Close()

	newDaemon := func(serverURL, profile, token string) *Daemon {
		d := &Daemon{
			cfg:    Config{ServerBaseURL: serverURL, DaemonID: "machine", Profile: profile},
			client: NewClient(serverURL),
			logger: slog.New(slog.DiscardHandler),
		}
		d.client.SetToken(token)
		return d
	}

	ctx := context.Background()
	owner, err := newDaemon(server.URL, "first", "first-token").preflightAuth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	second := newDaemon(server.URL, "second", "second-token")
	lock, err := second.preflightAuth(ctx)
	if err == nil {
		lock.Close()
		t.Fatal("second profile acquired the same runtimes")
	}
	if !strings.Contains(err.Error(), `profile "first"`) {
		t.Fatalf("startup error omitted owner profile: %v", err)
	}
	if got := workspaceSyncs.Load(); got != 1 {
		t.Fatalf("rejected daemon reached workspace sync: got %d syncs", got)
	}

	for _, d := range []*Daemon{
		newDaemon(server.URL, "other-account", "other-account"),
		newDaemon(otherServer.URL, "other-server", "first-token"),
	} {
		lock, err := d.preflightAuth(ctx)
		if err != nil {
			t.Fatalf("independent profile %q could not start: %v", d.cfg.Profile, err)
		}
		defer lock.Close()
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	successor, err := second.preflightAuth(ctx)
	if err != nil {
		t.Fatalf("successor could not acquire released runtimes: %v", err)
	}
	defer successor.Close()
}
