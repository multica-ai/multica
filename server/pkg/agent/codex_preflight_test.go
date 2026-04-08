package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCodexAppServerAcceptsSupportedCLI(t *testing.T) {
	t.Parallel()

	execPath := writeTestExecutable(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.119.0"
  exit 0
fi
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  echo "Usage: codex app-server --listen stdio://"
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := ensureCodexAppServer(context.Background(), execPath); err != nil {
		t.Fatalf("ensureCodexAppServer() error = %v", err)
	}
}

func TestEnsureCodexAppServerRejectsUnsupportedCLI(t *testing.T) {
	t.Parallel()

	execPath := writeTestExecutable(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.118.0"
  exit 0
fi
echo "unknown command \"app-server\"" >&2
exit 1
`)

	err := ensureCodexAppServer(context.Background(), execPath)
	if err == nil {
		t.Fatal("expected unsupported codex CLI to fail preflight")
	}
	if !strings.Contains(err.Error(), "detected version: codex-cli 0.118.0") {
		t.Fatalf("expected version in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Please upgrade codex-cli and restart the daemon") {
		t.Fatalf("expected upgrade hint in error, got %q", err.Error())
	}
}

func TestEnsureCodexAppServerCachesResultPerExecutable(t *testing.T) {
	countFile := filepath.Join(t.TempDir(), "count")
	execPath := writeTestExecutable(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.119.0"
  exit 0
fi
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  count=0
  if [ -f "` + countFile + `" ]; then
    count=$(cat "` + countFile + `")
  fi
  count=$((count+1))
  echo "$count" > "` + countFile + `"
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := ensureCodexAppServer(context.Background(), execPath); err != nil {
		t.Fatalf("first ensureCodexAppServer() error = %v", err)
	}
	if err := ensureCodexAppServer(context.Background(), execPath); err != nil {
		t.Fatalf("second ensureCodexAppServer() error = %v", err)
	}

	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "1" {
		t.Fatalf("app-server preflight count = %q, want %q", strings.TrimSpace(string(data)), "1")
	}
}

func writeTestExecutable(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write test executable: %v", err)
	}
	return path
}
