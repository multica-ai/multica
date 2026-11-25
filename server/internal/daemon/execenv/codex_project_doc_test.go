package execenv

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCodexProjectDocMaxBytesWritesDefaultWhenAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureCodexProjectDocMaxBytes(path, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "project_doc_max_bytes = 131072"
	if !strings.Contains(string(got), want) {
		t.Fatalf("missing %q in:\n%s", want, got)
	}
	if !strings.Contains(string(got), "model = \"gpt-5\"") {
		t.Fatalf("user config lost:\n%s", got)
	}
}

func TestEnsureCodexProjectDocMaxBytesPreservesExistingValue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "project_doc_max_bytes = 16000\nmodel = \"gpt-5\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureCodexProjectDocMaxBytes(path, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("user cap was rewritten:\n%s", got)
	}
}

func TestEnsureCodexProjectDocMaxBytesIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := ensureCodexProjectDocMaxBytes(path, nil); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureCodexProjectDocMaxBytes(path, nil); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second write changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Count(string(second), "project_doc_max_bytes") != 1 {
		t.Fatalf("expected one project_doc_max_bytes line, got:\n%s", second)
	}
}

func TestEnsureCodexProjectDocMaxBytesIgnoresProfileLevelKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "model = \"gpt-5\"\n\n[profiles.work]\nproject_doc_max_bytes = 16000\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureCodexProjectDocMaxBytes(path, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "project_doc_max_bytes = 131072") {
		t.Fatalf("profile-only key should not skip the top-level raise:\n%s", text)
	}
	if !strings.Contains(text, "[profiles.work]") || !strings.Contains(text, "project_doc_max_bytes = 16000") {
		t.Fatalf("profile table was rewritten:\n%s", text)
	}
	if strings.Count(text, "project_doc_max_bytes") != 2 {
		t.Fatalf("expected top-level raise plus preserved profile key, got:\n%s", text)
	}
}

func TestWarnIfRuntimeInstructionFileExceedsUserCodexCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	codexHome := filepath.Join(dir, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("project_doc_max_bytes = 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 120)), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	warnIfRuntimeInstructionFileExceedsCap(path, "codex", TaskContextForEnv{
		Logger:    logger,
		CodexHome: codexHome,
	})
	out := buf.String()
	if !strings.Contains(out, "exceeds the runtime's size cap") {
		t.Fatalf("expected a cap warning, got %q", out)
	}
	if !strings.Contains(out, "cap=100") {
		t.Fatalf("warning should use the user's top-level cap, got %q", out)
	}
	if strings.Contains(out, "cap=131072") || strings.Contains(out, "cap=32768") {
		t.Fatalf("warning used the raised or default cap instead of the user cap: %q", out)
	}
}
