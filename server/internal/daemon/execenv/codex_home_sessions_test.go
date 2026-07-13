package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// seedFakeRollout writes a fake Codex rollout for sessionID under
// sharedSessions/YYYY/MM/DD, mirroring Codex's real layout, and returns its path.
func seedFakeRollout(t *testing.T, sharedSessions, y, m, d, sessionID string, size int) string {
	t.Helper()
	dir := filepath.Join(sharedSessions, y, m, d)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	path := filepath.Join(dir, "rollout-"+y+"-"+m+"-"+d+"T00-00-00-"+sessionID+".jsonl")
	body := make([]byte, size)
	for i := range body {
		body[i] = 'x'
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

// seedLegacySessionsSymlink recreates the pre-MUL-4424 layout: codex-home's
// sessions is a symlink into the shared ~/.codex/sessions. Skips on Windows
// sessions where symlink creation is unavailable.
func seedLegacySessionsSymlink(t *testing.T, codexHome, sharedSessions string) {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	if err := os.MkdirAll(sharedSessions, 0o755); err != nil {
		t.Fatalf("mkdir shared sessions: %v", err)
	}
	if err := os.Symlink(sharedSessions, filepath.Join(codexHome, "sessions")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("seed legacy sessions symlink: %v", err)
	}
}

func TestPrepareCodexSessionsDir_FreshCreatesEmptyLocalDir(t *testing.T) {
	t.Parallel()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	sharedHome := t.TempDir()

	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("fresh sessions must be a real dir, not a symlink")
	}
	if !fi.IsDir() {
		t.Error("fresh sessions must be a directory")
	}
	entries, _ := os.ReadDir(sessions)
	if len(entries) != 0 {
		t.Errorf("fresh sessions must be empty, has %d entries", len(entries))
	}
}

func TestPrepareCodexSessionsDir_RealDirIsAuthoritative(t *testing.T) {
	t.Parallel()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	sessions := filepath.Join(codexHome, "sessions", "2026", "07", "13")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	// A rollout the prior (isolated) run already wrote into the task-local dir.
	own := filepath.Join(sessions, "rollout-2026-07-13T00-00-00-own-session.jsonl")
	if err := os.WriteFile(own, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write own rollout: %v", err)
	}

	if err := prepareCodexSessionsDir(codexHome, t.TempDir(), CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	// The task-local dir is authoritative — its contents must survive untouched.
	if data, err := os.ReadFile(own); err != nil || string(data) != "keep me" {
		t.Errorf("task-local rollout must be preserved, got err=%v data=%q", err, data)
	}
	fi, _ := os.Lstat(filepath.Join(codexHome, "sessions"))
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("authoritative sessions must remain a real dir")
	}
}

func TestPrepareCodexSessionsDir_MigratesLegacySymlinkNoResume(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	sharedSessions := filepath.Join(root, "shared", "sessions")
	// Simulate a machine with accumulated global history.
	seedFakeRollout(t, sharedSessions, "2026", "07", "13", "other-session-a", 16)
	seedFakeRollout(t, sharedSessions, "2026", "07", "12", "other-session-b", 16)
	seedLegacySessionsSymlink(t, codexHome, sharedSessions)

	// Session-derived state that indexed the whole global history, plus an
	// unrelated per-task DB that must NOT be touched.
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite"), "stale")
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite-wal"), "stale")
	writeFile(t, filepath.Join(codexHome, "session_index.jsonl"), "stale")
	writeFile(t, filepath.Join(codexHome, "goals_1.sqlite"), "keep")

	if err := prepareCodexSessionsDir(codexHome, filepath.Dir(sharedSessions), CodexHomeOptions{}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions missing after migration: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("legacy symlink must be replaced with a real dir")
	}
	// No resume requested -> none of the global history is pulled in.
	entries, _ := os.ReadDir(sessions)
	if len(entries) != 0 {
		t.Errorf("non-resume migration must yield an empty sessions dir, has %d entries", len(entries))
	}
	// The global history itself must be left intact.
	if _, err := os.Stat(filepath.Join(sharedSessions, "2026", "07", "13", "rollout-2026-07-13T00-00-00-other-session-a.jsonl")); err != nil {
		t.Errorf("shared history must not be deleted: %v", err)
	}
	// Session-derived state dropped; unrelated DB preserved.
	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite"))
	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite-wal"))
	assertAbsent(t, filepath.Join(codexHome, "session_index.jsonl"))
	assertPresent(t, filepath.Join(codexHome, "goals_1.sqlite"))
}

func TestPrepareCodexSessionsDir_MigrateWithResumeExposesOnlyThatRollout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	sharedSessions := filepath.Join(root, "shared", "sessions")
	resumeID := "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	resumeSrc := seedFakeRollout(t, sharedSessions, "2026", "07", "13", resumeID, 32)
	seedFakeRollout(t, sharedSessions, "2026", "07", "11", "unrelated-session", 32)
	seedLegacySessionsSymlink(t, codexHome, sharedSessions)
	writeFile(t, filepath.Join(codexHome, "state_5.sqlite"), "stale")

	err := prepareCodexSessionsDir(codexHome, filepath.Dir(sharedSessions), CodexHomeOptions{ResumeSessionID: resumeID}, testLogger())
	if err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}

	// The resumed rollout is exposed at the same YYYY/MM/DD relative path.
	exposed := filepath.Join(codexHome, "sessions", "2026", "07", "13", filepath.Base(resumeSrc))
	li, err := os.Lstat(exposed)
	if err != nil {
		t.Fatalf("resume rollout not exposed: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Error("resume rollout must be a symlink (never a copy) to avoid an unbounded copy on the critical path")
	}
	if data, err := os.ReadFile(exposed); err != nil || len(data) != 32 {
		t.Errorf("resume rollout must be readable through the link, err=%v len=%d", err, len(data))
	}
	// The unrelated session must NOT be pulled in.
	assertAbsent(t, filepath.Join(codexHome, "sessions", "2026", "07", "11", "rollout-2026-07-11T00-00-00-unrelated-session.jsonl"))
	// Stale state dropped so Codex rebuilds from the single exposed rollout.
	assertAbsent(t, filepath.Join(codexHome, "state_5.sqlite"))
}

func TestExposeResumeRollout_NoMatchReturnsError(t *testing.T) {
	t.Parallel()
	shared := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	local := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := exposeResumeRollout(shared, local, "missing-session", testLogger()); err == nil {
		t.Error("expected error when no rollout matches the resume session ID")
	}
}

func TestExposeResumeRollout_LinksLargeFileWithoutCopying(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	shared := filepath.Join(root, "shared", "sessions")
	local := filepath.Join(root, "local", "sessions")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	// A deliberately large rollout — copying it onto the critical path is
	// exactly what MUL-4424 forbids.
	const big = 4 << 20 // 4 MiB
	seedFakeRollout(t, shared, "2026", "07", "13", "big-session", big)

	if err := exposeResumeRollout(shared, local, "big-session", testLogger()); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("exposeResumeRollout: %v", err)
	}

	dst := filepath.Join(local, "2026", "07", "13", "rollout-2026-07-13T00-00-00-big-session.jsonl")
	li, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("exposed rollout missing: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatal("large rollout must be symlinked, not copied")
	}
	// A symlink's own size is the link target length, never the 4 MiB payload.
	if li.Size() >= big {
		t.Errorf("dst looks like a full copy (size=%d); must be a symlink", li.Size())
	}
}

func TestResetCodexSessionState_OnlyRemovesSessionDerived(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	sessionDerived := []string{
		"state_5.sqlite", "state_5.sqlite-shm", "state_5.sqlite-wal",
		"state_6.sqlite", "session_index.jsonl",
	}
	preserved := []string{
		"goals_1.sqlite", "logs_2.sqlite", "memories_1.sqlite",
		"config.toml", "auth.json", "models_cache.json",
	}
	for _, n := range append(append([]string{}, sessionDerived...), preserved...) {
		writeFile(t, filepath.Join(home, n), "x")
	}

	resetCodexSessionState(home, testLogger())

	for _, n := range sessionDerived {
		assertAbsent(t, filepath.Join(home, n))
	}
	for _, n := range preserved {
		assertPresent(t, filepath.Join(home, n))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, err=%v", filepath.Base(path), err)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("expected %s to be preserved: %v", filepath.Base(path), err)
	}
}
