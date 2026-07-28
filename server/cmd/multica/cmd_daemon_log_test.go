package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnvPositiveIntOrDefault locks in the parse-or-fallback behaviour the log
// rotation knobs rely on. Critically, an explicit 0 falls back to the default
// (not through to lumberjack, where 0 means 100MB / keep-everything), so the
// retention footguns can't be tripped from the environment.
func TestEnvPositiveIntOrDefault(t *testing.T) {
	const key = "MULTICA_TEST_ENV_INT"
	cases := []struct {
		name string
		set  bool
		val  string
		def  int
		want int
	}{
		{"unset", false, "", 20, 20},
		{"blank", true, "", 20, 20},
		{"whitespace", true, "   ", 20, 20},
		{"valid", true, "50", 20, 50},
		{"zero_falls_back", true, "0", 20, 20},
		{"negative", true, "-1", 20, 20},
		{"malformed", true, "abc", 20, 20},
		{"padded", true, "  7 ", 20, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Unsetenv(key)
			if c.set {
				t.Setenv(key, c.val)
			}
			if got := envPositiveIntOrDefault(key, c.def); got != c.want {
				t.Errorf("envPositiveIntOrDefault(%q, %d) = %d, want %d", c.val, c.def, got, c.want)
			}
		})
	}
}

// TestOpenBoundedErrLogRolls verifies the raw crash sink is bounded: once it is
// at/over the cap, opening it rolls the old contents to a single ".1" backup
// and starts fresh, so a crash loop can't grow daemon.err.log without limit.
func TestOpenBoundedErrLogRolls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.err.log")

	// Seed an over-cap file.
	big := make([]byte, errLogMaxBytes+1024)
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f, err := openBoundedErrLog(path)
	if err != nil {
		t.Fatalf("openBoundedErrLog: %v", err)
	}
	f.Close()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected rolled backup %s.1: %v", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat active: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("active err log = %d bytes after roll, want 0", fi.Size())
	}
}

// TestOpenBoundedErrLogKeepsSmall confirms a below-cap file is appended to, not
// rolled — the common healthy-daemon case.
func TestOpenBoundedErrLogKeepsSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.err.log")
	if err := os.WriteFile(path, []byte("prior crash\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f, err := openBoundedErrLog(path)
	if err != nil {
		t.Fatalf("openBoundedErrLog: %v", err)
	}
	f.WriteString("next line\n")
	f.Close()

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("did not expect a rolled backup for a small file")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "prior crash") || !strings.Contains(string(data), "next line") {
		t.Errorf("expected append, got %q", data)
	}
}

// TestNewDaemonLogRotatorDefaults asserts the rotator is wired to the intended
// policy when no env overrides are set: the configured path, the default
// size/backups/age, and gzip compression so rotated files stay small.
func TestNewDaemonLogRotatorDefaults(t *testing.T) {
	os.Unsetenv("MULTICA_DAEMON_LOG_MAX_SIZE_MB")
	os.Unsetenv("MULTICA_DAEMON_LOG_MAX_BACKUPS")
	os.Unsetenv("MULTICA_DAEMON_LOG_MAX_AGE_DAYS")

	path := filepath.Join(t.TempDir(), "daemon.log")
	r := newDaemonLogRotator(path)
	if r.Filename != path {
		t.Errorf("Filename = %q, want %q", r.Filename, path)
	}
	if r.MaxSize != defaultDaemonLogMaxSizeMB {
		t.Errorf("MaxSize = %d, want %d", r.MaxSize, defaultDaemonLogMaxSizeMB)
	}
	if r.MaxBackups != defaultDaemonLogMaxBackups {
		t.Errorf("MaxBackups = %d, want %d", r.MaxBackups, defaultDaemonLogMaxBackups)
	}
	if r.MaxAge != defaultDaemonLogMaxAgeDays {
		t.Errorf("MaxAge = %d, want %d", r.MaxAge, defaultDaemonLogMaxAgeDays)
	}
	if !r.Compress {
		t.Error("Compress = false, want true")
	}
}

// TestNewDaemonLogRotatorEnvOverride confirms operators can tune retention via
// env without a rebuild.
func TestNewDaemonLogRotatorEnvOverride(t *testing.T) {
	t.Setenv("MULTICA_DAEMON_LOG_MAX_SIZE_MB", "5")
	t.Setenv("MULTICA_DAEMON_LOG_MAX_BACKUPS", "2")
	t.Setenv("MULTICA_DAEMON_LOG_MAX_AGE_DAYS", "7")

	r := newDaemonLogRotator(filepath.Join(t.TempDir(), "daemon.log"))
	if r.MaxSize != 5 || r.MaxBackups != 2 || r.MaxAge != 7 {
		t.Errorf("rotator = {MaxSize:%d MaxBackups:%d MaxAge:%d}, want {5 2 7}", r.MaxSize, r.MaxBackups, r.MaxAge)
	}
}

// TestDaemonLogRotatorRotates is the end-to-end guarantee: once the active file
// crosses MaxSize the writer rotates instead of appending forever, so daemon.log
// stays bounded (the core of MUL-4330). Uses the 1 MB floor lumberjack enforces.
func TestDaemonLogRotatorRotates(t *testing.T) {
	t.Setenv("MULTICA_DAEMON_LOG_MAX_SIZE_MB", "1")
	t.Setenv("MULTICA_DAEMON_LOG_MAX_BACKUPS", "3")
	// Disable compression here so the assertion on the active file size is not
	// racing lumberjack's async gzip of the rotated file.
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	r := newDaemonLogRotator(path)
	r.Compress = false

	// Write ~1.5 MB in chunks; at least one rotation must happen.
	line := strings.Repeat("x", 1024) + "\n"
	for written := 0; written < 3*1024*1024; written += len(line) {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var logFiles int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "daemon") && strings.Contains(e.Name(), ".log") {
			logFiles++
		}
	}
	if logFiles < 2 {
		t.Fatalf("expected at least 2 log files after rotation, got %d (%v)", logFiles, entries)
	}

	// The active file must be bounded well under the total bytes written.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat active log: %v", err)
	}
	if info.Size() > 2*1024*1024 {
		t.Errorf("active daemon.log = %d bytes, expected bounded (<2MB)", info.Size())
	}
}

// TestDaemonStderrLogPathIsSeparate guards the invariant that the raw crash
// sink is a distinct file from the rotating log, so the child's inherited fds
// never hold daemon.log open (which would block rotation's rename on Windows).
func TestDaemonStderrLogPathIsSeparate(t *testing.T) {
	logPath := daemonLogPathForProfile("")
	errPath := daemonStderrLogPathForProfile("")
	if logPath == "" || errPath == "" {
		t.Skip("profile dir unavailable in this environment")
	}
	if logPath == errPath {
		t.Fatalf("stderr sink %q must differ from daemon.log %q", errPath, logPath)
	}
	if filepath.Base(errPath) != "daemon.err.log" {
		t.Errorf("stderr sink base = %q, want daemon.err.log", filepath.Base(errPath))
	}
	if filepath.Dir(logPath) != filepath.Dir(errPath) {
		t.Errorf("sinks should share a directory: %q vs %q", filepath.Dir(logPath), filepath.Dir(errPath))
	}
}
