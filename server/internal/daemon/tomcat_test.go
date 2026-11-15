package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestTomcatDiscoveryHonorsOverrideAndModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test executable")
	}
	home := t.TempDir()
	redirectTestHome(t, home)
	bin := filepath.Join(t.TempDir(), "tomcat")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'tomcat 0.1.80\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)
	t.Setenv("MULTICA_CODEX_PATH", filepath.Join(home, "missing-codex"))
	t.Setenv("MULTICA_TOMCAT_PATH", bin)
	t.Setenv("MULTICA_TOMCAT_MODEL", "  configured-model  ")
	cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := cfg.Agents["tomcat"]; !ok || entry.Path != bin || entry.Command != bin || entry.Model != "configured-model" {
		t.Fatalf("Tomcat entry: %+v", cfg.Agents["tomcat"])
	}
	if providerNeedsInlineSystemPrompt("tomcat") {
		t.Fatal("Tomcat brief must stay in AGENTS.md")
	}
}

// A forwarder can legitimately need 35s for its bounded shared-service close.
// Missing the daemon's shorter handoff must preserve, not authorize, cleanup.
type tomcatSlowCleanupBackend struct {
	gate   <-chan struct{}
	cancel context.CancelFunc
}

func (b tomcatSlowCleanupBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message)
	results := make(chan agent.Result, 1)
	go func() {
		<-ctx.Done()
		close(messages)
		<-b.gate
		time.Sleep(35 * time.Second)
		results <- agent.Result{Status: "aborted"}
		close(results)
	}()
	b.cancel()
	return &agent.Session{Messages: messages, Result: results, CleanupRequired: true, TerminalObserved: func() bool { return false }}, nil
}

func TestTomcatCleanupBeyondHandoffFailsClosed(t *testing.T) {
	// Keep the real HTTP server outside the virtual-time bubble.
	d := newTestDaemon(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		probe := &handoffProbe{gate: make(chan struct{}), prefix: "waiting for the backend to hand over its result after cancellation"}
		result, _, err := d.executeAndDrain(ctx, tomcatSlowCleanupBackend{gate: probe.gate, cancel: cancel}, "task", agent.ExecOptions{}, slog.New(probe), "tomcat-slow-cleanup", "", new(atomic.Int32))
		if !probe.seen.Load() || err != nil || result.Status != "cleanup_unconfirmed" {
			t.Fatalf("handoff=%v result=%+v err=%v", probe.seen.Load(), result, err)
		}
		if d.ShutdownCleanupConfirmed() || shouldRetryWithFreshSession(result, "existing-session", 0, "tomcat") {
			t.Fatal("missing cleanup acknowledgement allowed automatic continuation")
		}
		// Join the delayed acknowledgement without advancing a real clock.
		time.Sleep(6 * time.Second)
		synctest.Wait()
	})
}
