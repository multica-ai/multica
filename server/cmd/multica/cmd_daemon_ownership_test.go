package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// TestTakeoverDaemonOwner_ShutsDownAndWaits covers supported takeover: the
// launcher asks the incumbent to stop via its recorded health port, waits for
// it to release the machine lock, and then the lock is free for the successor.
// The incumbent is stood in for by a held same-process lock that a fake
// /shutdown endpoint releases (flock treats two opens of one file as
// contending, even in one process).
func TestTakeoverDaemonOwner_ShutsDownAndWaits(t *testing.T) {
	baseDir := t.TempDir()

	held, err := daemon.AcquireOwnership(baseDir, daemon.OwnerInfo{PID: 4242, Version: "0.4.2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed incumbent lock: %v", err)
	}
	var releaseOnce sync.Once
	defer held.Release()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			releaseOnce.Do(func() { _ = held.Release() })
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	if err := takeoverDaemonOwner(baseDir, daemon.OwnerInfo{PID: 4242, HealthPort: port}); err != nil {
		t.Fatalf("takeover should succeed once the incumbent releases: %v", err)
	}

	// The lock must now be free for the successor.
	free, _, _, err := daemon.ProbeOwnership(baseDir)
	if err != nil {
		t.Fatalf("probe after takeover: %v", err)
	}
	if !free {
		t.Fatal("lock should be free after a successful takeover")
	}
}

// TestTakeoverDaemonOwner_NoHealthPort covers the case where the incumbent
// recorded no reachable health port: takeover cannot proceed and must return an
// actionable error rather than hang.
func TestTakeoverDaemonOwner_NoHealthPort(t *testing.T) {
	baseDir := t.TempDir()
	err := takeoverDaemonOwner(baseDir, daemon.OwnerInfo{PID: 4242, HealthPort: 0})
	if err == nil {
		t.Fatal("takeover with no health port must error")
	}
}

// TestDetectUnlockedDaemon covers the rolling-upgrade blind spot: a daemon
// running WITHOUT the machine lock (a pre-lock release, or one started under
// the break-glass env) must block a new daemon from starting alongside it.
// Port liveness is stubbed so the test never touches real well-known ports —
// a developer machine may have an actual daemon on them.
func TestDetectUnlockedDaemon(t *testing.T) {
	restore := probeDaemonHealth
	t.Cleanup(func() { probeDaemonHealth = restore })

	nativeAlive := map[string]any{"status": "running", "os": runtime.GOOS}
	dead := map[string]any{}
	foreignOS := "windows"
	if runtime.GOOS == "windows" {
		foreignOS = "linux"
	}

	t.Run("refuses while an unlocked daemon is alive", func(t *testing.T) {
		baseDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(baseDir, "profiles", "desktop-host"), 0o755); err != nil {
			t.Fatal(err)
		}
		legacyPort := healthPortForProfile("desktop-host")
		probeDaemonHealth = func(port int) map[string]any {
			if port == legacyPort {
				return nativeAlive
			}
			return dead
		}

		err := detectUnlockedDaemon(baseDir)
		if err == nil {
			t.Fatal("an alive unlocked daemon must refuse the start")
		}
		for _, want := range []string{"--profile desktop-host", "MULTICA_DAEMON_ALLOW_MULTIPLE"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error missing %q: %v", want, err)
			}
		}
	})

	t.Run("clean when nothing is alive", func(t *testing.T) {
		baseDir := t.TempDir()
		probeDaemonHealth = func(int) map[string]any { return dead }
		if err := detectUnlockedDaemon(baseDir); err != nil {
			t.Fatalf("no live daemons must mean no error: %v", err)
		}
	})

	t.Run("skips a foreign-environment daemon", func(t *testing.T) {
		// #3916 topology: a daemon visible through localhost forwarding but
		// running under a different OS (e.g. WSL2 behind Windows). Its
		// lifecycle is not this launcher's to gate on — must not refuse.
		baseDir := t.TempDir()
		probeDaemonHealth = func(int) map[string]any {
			return map[string]any{"status": "running", "os": foreignOS}
		}
		if err := detectUnlockedDaemon(baseDir); err != nil {
			t.Fatalf("a foreign-OS daemon must not block startup: %v", err)
		}
	})

	t.Run("fails safe when the daemon reports no OS", func(t *testing.T) {
		// An older release that predates the os field is exactly the legacy
		// daemon the sweep exists to catch — missing os must refuse.
		baseDir := t.TempDir()
		probeDaemonHealth = func(int) map[string]any {
			return map[string]any{"status": "running"}
		}
		if err := detectUnlockedDaemon(baseDir); err == nil {
			t.Fatal("a live daemon with no reported OS must refuse the start")
		}
	})

	t.Run("break-glass bypasses the sweep", func(t *testing.T) {
		baseDir := t.TempDir()
		probeDaemonHealth = func(int) map[string]any { return nativeAlive }
		t.Setenv("MULTICA_DAEMON_ALLOW_MULTIPLE", "1")
		if err := detectUnlockedDaemon(baseDir); err != nil {
			t.Fatalf("break-glass must bypass the sweep: %v", err)
		}
	})
}

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port from %q: %v", rawURL, err)
	}
	return p
}
