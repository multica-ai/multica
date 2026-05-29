package traceupload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocateClaude(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	projDir := filepath.Join(configDir, "projects", "-some-workdir-slug")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(projDir, "sess-abc.jsonl")
	if err := os.WriteFile(want, []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LocateJSONL(LocateParams{Runtime: "claude", SessionID: "sess-abc"})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestLocateClaudeMissing(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if _, err := LocateJSONL(LocateParams{Runtime: "claude", SessionID: "nope"}); err == nil {
		t.Fatal("expected error for missing session")
	}
	if _, err := LocateJSONL(LocateParams{Runtime: "claude", SessionID: ""}); err == nil {
		t.Fatal("expected error for empty session id")
	}
}

func TestLocateCodex(t *testing.T) {
	envRoot := t.TempDir()
	sessDir := filepath.Join(envRoot, "codex-home", "sessions", "2026", "05", "29")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sessDir, "rollout-2026-05-29T12-00-00-uuid.jsonl")
	if err := os.WriteFile(want, []byte(`{"type":"token_count"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LocateJSONL(LocateParams{Runtime: "codex", EnvRoot: envRoot})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	// The locator resolves the sessions symlink (EvalSymlinks), which on macOS
	// also canonicalizes /tmp → /private/tmp; resolve want the same way.
	if got != resolve(t, want) {
		t.Errorf("got %q want %q", got, resolve(t, want))
	}
}

// resolve canonicalizes a path the same way the locator does, so tests on
// platforms where the temp dir is itself a symlink (macOS /tmp) still match.
func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

// TestLocateCodexSymlinkedSessions reproduces the real daemon layout: the
// per-task codex-home/sessions is a SYMLINK into a shared sessions tree that
// holds rollouts from many concurrent tasks. The locator must (a) follow the
// symlink — filepath.WalkDir won't, so without EvalSymlinks it finds nothing —
// and (b) return THIS task's rollout (matched by WorkDir/cwd), not whichever
// sibling happens to be newest.
func TestLocateCodexSymlinkedSessions(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "shared", "sessions", "2026", "05", "29")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}

	mine := filepath.Join(base, "workdirs", "task-mine")
	other := filepath.Join(base, "workdirs", "task-other")

	// A NEWER sibling rollout from a concurrent task (different cwd). Newest by
	// mtime, so a naive newest-file locator would wrongly return this one.
	otherRollout := filepath.Join(shared, "rollout-2026-05-29T13-00-00-other.jsonl")
	if err := os.WriteFile(otherRollout, []byte(`{"type":"session_meta","payload":{"id":"other","cwd":"`+other+`"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// My (older) rollout.
	myRollout := filepath.Join(shared, "rollout-2026-05-29T12-00-00-mine.jsonl")
	if err := os.WriteFile(myRollout, []byte(`{"type":"session_meta","payload":{"id":"mine","cwd":"`+mine+`"}}`+"\n{\"type\":\"token_count\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(myRollout, past, past); err != nil {
		t.Fatal(err)
	}

	// Per-task codex-home with sessions symlinked to the shared tree.
	envRoot := t.TempDir()
	codexHome := filepath.Join(envRoot, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "shared", "sessions"), filepath.Join(codexHome, "sessions")); err != nil {
		t.Fatal(err)
	}

	got, err := LocateJSONL(LocateParams{Runtime: "codex", EnvRoot: envRoot, WorkDir: mine})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != resolve(t, myRollout) {
		t.Errorf("cwd disambiguation failed: got %q want %q", got, resolve(t, myRollout))
	}

	// Without a WorkDir hint, fall back to newest (the sibling) — still finds a
	// file (proves the symlink was followed) rather than erroring.
	gotNoHint, err := LocateJSONL(LocateParams{Runtime: "codex", EnvRoot: envRoot})
	if err != nil {
		t.Fatalf("locate (no hint): %v", err)
	}
	if gotNoHint != resolve(t, otherRollout) {
		t.Errorf("newest fallback: got %q want %q", gotNoHint, resolve(t, otherRollout))
	}
}

func TestLocateCodexEmptyRoot(t *testing.T) {
	if _, err := LocateJSONL(LocateParams{Runtime: "codex", EnvRoot: ""}); err == nil {
		t.Fatal("expected error for empty env root")
	}
}

func TestLocateUnsupportedRuntime(t *testing.T) {
	if _, err := LocateJSONL(LocateParams{Runtime: "gemini", SessionID: "x"}); err == nil {
		t.Fatal("expected error for unsupported runtime")
	}
}
