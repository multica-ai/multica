package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type depSyncLogRecord struct {
	Level          string `json:"level"`
	Msg            string `json:"msg"`
	HermesOutput   string `json:"hermes_output"`
	HermesHome     string `json:"hermes_home"`
	StartupDelayMs int64  `json:"startup_delay_ms"`
	HandshakeOK    *bool  `json:"handshake_ok"`
}

func decodeDepSyncLogs(t *testing.T, buf *bytes.Buffer) []depSyncLogRecord {
	t.Helper()
	var out []depSyncLogRecord
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var r depSyncLogRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if strings.Contains(r.Msg, "dependency") {
			out = append(out, r)
		}
	}
	return out
}

func newTestDepSyncWatcher(buf *bytes.Buffer) (*hermesDepSyncWatcher, *time.Time) {
	clock := new(time.Time)
	*clock = time.Unix(1_700_000_000, 0)
	w := newHermesDepSyncWatcherAt(slog.New(slog.NewJSONHandler(buf, nil)), "/tmp/task/hermes-home",
		func() time.Time { return *clock })
	return w, clock
}

// Every launch-time reinstall message Hermes prints (hermes_cli/venv_sync.py)
// is detected, logged once at Warn with the line and home, and the finish log
// carries the measured startup delay.
func TestHermesDepSyncWatcherDetectsReinstallMarkers(t *testing.T) {
	t.Parallel()
	lines := []string{
		"hermes: completing source-update dependencies...",
		"hermes: preparing dependencies for this update...",
		"hermes: preparing dependencies for this data root (the checkout's updates belong to C:\\Users\\u\\AppData\\Local\\hermes)...",
	}
	for _, line := range lines {
		line := line
		t.Run(line[:40], func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			w, clock := newTestDepSyncWatcher(&buf)
			_, _ = w.Write([]byte("some unrelated banner\r\n" + line + "\r\n"))
			if !w.Detected() {
				t.Fatalf("marker %q not detected", line)
			}
			*clock = clock.Add(47 * time.Second)
			w.Finish(true)

			recs := decodeDepSyncLogs(t, &buf)
			if len(recs) != 2 {
				t.Fatalf("want start+finish logs, got %d: %s", len(recs), buf.String())
			}
			if recs[0].Level != "WARN" || recs[0].HermesOutput != line || recs[0].HermesHome != "/tmp/task/hermes-home" {
				t.Errorf("unexpected start record: %+v", recs[0])
			}
			if recs[1].StartupDelayMs != 47000 || recs[1].HandshakeOK == nil || !*recs[1].HandshakeOK {
				t.Errorf("unexpected finish record: %+v", recs[1])
			}
		})
	}
}

// A marker split across writes is still matched; repeat markers and repeat
// Finish calls log only once.
func TestHermesDepSyncWatcherPartialWritesAndOnce(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w, _ := newTestDepSyncWatcher(&buf)
	_, _ = w.Write([]byte("hermes: completing source-upd"))
	if w.Detected() {
		t.Fatal("partial line must not be matched before its newline")
	}
	_, _ = w.Write([]byte("ate dependencies...\n"))
	_, _ = w.Write([]byte("hermes: preparing dependencies for this update...\n"))
	w.Finish(false)
	w.Finish(true)

	recs := decodeDepSyncLogs(t, &buf)
	if len(recs) != 2 {
		t.Fatalf("want exactly one start and one finish log, got %d: %s", len(recs), buf.String())
	}
	if recs[1].HandshakeOK == nil || *recs[1].HandshakeOK {
		t.Errorf("finish should record the first (failed) handshake, got %+v", recs[1])
	}
}

// Normal startup output, including log records that merely mention
// dependencies, never triggers the warning, and Finish without a detection is
// silent.
func TestHermesDepSyncWatcherIgnoresUnrelatedOutput(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w, _ := newTestDepSyncWatcher(&buf)
	_, _ = w.Write([]byte(strings.Join([]string{
		"2026-10-21 09:54:01,123 [INFO] root: user said: hermes: completing source-update dependencies",
		"venv sync: current",
		"⚠️  API call failed (attempt 1/3): retryable upstream blip",
		"",
	}, "\n")))
	w.Finish(true)
	if w.Detected() {
		t.Fatal("unrelated output must not be detected as a reinstall")
	}
	if recs := decodeDepSyncLogs(t, &buf); len(recs) != 0 {
		t.Fatalf("expected no logs, got %s", buf.String())
	}
}

// An oversized unterminated line is dropped instead of buffered without bound.
func TestHermesDepSyncWatcherBoundsPartialLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w, _ := newTestDepSyncWatcher(&buf)
	_, _ = w.Write(bytes.Repeat([]byte("x"), hermesDepSyncRemainsCap+10))
	w.mu.Lock()
	n := len(w.remains)
	w.mu.Unlock()
	if n > hermesDepSyncRemainsCap {
		t.Fatalf("partial-line buffer grew to %d bytes", n)
	}
	_, _ = w.Write([]byte("\nhermes: completing source-update dependencies...\n"))
	if !w.Detected() {
		t.Fatal("marker after a dropped oversized line should still be detected")
	}
}

// stdout and stderr are drained independently, so the handshake can be
// handled before the stderr copier reaches a marker Hermes wrote first. The
// finish log must still be emitted, with the delay measured from launch to the
// handshake rather than from when the marker line happened to be processed.
func TestHermesDepSyncWatcherHandshakeBeforeMarker(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w, clock := newTestDepSyncWatcher(&buf)
	*clock = clock.Add(52 * time.Second)
	w.Finish(true)
	if recs := decodeDepSyncLogs(t, &buf); len(recs) != 0 {
		t.Fatalf("no logs expected before a marker is seen, got %s", buf.String())
	}
	*clock = clock.Add(3 * time.Second)
	_, _ = w.Write([]byte("hermes: completing source-update dependencies...\n"))

	recs := decodeDepSyncLogs(t, &buf)
	if len(recs) != 2 {
		t.Fatalf("want start+finish logs, got %d: %s", len(recs), buf.String())
	}
	if recs[1].StartupDelayMs != 52000 || recs[1].HandshakeOK == nil || !*recs[1].HandshakeOK {
		t.Errorf("finish should report launch-to-handshake delay: %+v", recs[1])
	}
}
