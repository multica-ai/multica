package execenv

import (
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
