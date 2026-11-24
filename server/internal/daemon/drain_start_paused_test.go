package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_StartPaused(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{{"", false}, {"false", false}, {"true", true}, {"1", true}}
	for _, tc := range cases {
		t.Run("env="+tc.env, func(t *testing.T) {
			stageFakeAgent(t)
			t.Setenv("MULTICA_DAEMON_START_PAUSED", tc.env)
			cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:8080", WorkspacesRoot: t.TempDir()})
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.StartPaused != tc.want {
				t.Fatalf("StartPaused = %v, want %v", cfg.StartPaused, tc.want)
			}
		})
	}
}

func TestStartPausedDaemonClaimsNothingUntilResume(t *testing.T) {
	d := New(Config{StartPaused: true, WorkspacesRoot: t.TempDir()}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	if d.tryEnterClaim() {
		t.Fatal("daemon started paused admitted a claim")
	}
	rec := httptest.NewRecorder()
	d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/drain", nil))
	got := decodeDrainResponse(t, rec)
	if got.Status != "drained" || !got.StartedPaused {
		t.Fatalf("GET /drain = %+v, want drained and started_paused", got)
	}

	// A drain call on a daemon that started paused reports drained right away
	// and does not lift the pause.
	if rec := postDrain(d, `{"timeout_ms":100}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /drain = %d, want 200", rec.Code)
	}
	if d.tryEnterClaim() {
		t.Fatal("POST /drain lifted the start pause")
	}

	rec = httptest.NewRecorder()
	d.resumeHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/resume", nil))
	if got := decodeDrainResponse(t, rec); rec.Code != http.StatusOK || got.Status != "running" || got.StartedPaused {
		t.Fatalf("POST /resume = %d %+v, want running", rec.Code, got)
	}
	if !d.tryEnterClaim() {
		t.Fatal("resume did not restore admission")
	}
	d.exitClaim()
}

func TestDaemonDoesNotStartPausedByDefault(t *testing.T) {
	d := New(Config{WorkspacesRoot: t.TempDir()}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if !d.tryEnterClaim() {
		t.Fatal("default daemon refused a claim")
	}
	d.exitClaim()
	if got := d.drainSnapshot(); got.Status != "running" || got.StartedPaused {
		t.Fatalf("snapshot = %+v, want running", got)
	}
}

func TestStartPausedWarnsUntilResumed(t *testing.T) {
	logs := &lockedBuffer{}
	d := New(Config{StartPaused: true, WorkspacesRoot: t.TempDir()}, slog.New(slog.NewTextHandler(logs, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { d.warnWhileStartPaused(ctx, 5*time.Millisecond); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for strings.Count(logs.String(), "started paused") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no repeated warning; logs=%s", logs.String())
		}
		time.Sleep(time.Millisecond)
	}
	d.resumeHandler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/resume", nil))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("warning loop kept running after resume")
	}
}
