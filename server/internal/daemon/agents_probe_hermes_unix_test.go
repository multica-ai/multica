//go:build !windows

package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MUL-9132: the registration probe must reach Hermes through `acp --version`.
// The root `hermes --version` checks GitHub for updates; on a slow network that
// outlasted the probe timeout every round and left a partial git pack behind
// each time.
func TestProbeBuiltinRuntimeProbesHermesThroughACP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermes")
	record := filepath.Join(dir, "argv.log")
	body := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "acp" ] && [ "$2" = "--version" ]; then echo 0.22.5; exit 0; fi
echo "Hermes Agent v0.22.5 (2026.9.24)"
`, record)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}

	d := &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentVersions: map[string]string{},
	}
	version, reason, verdict := d.probeBuiltinRuntime(context.Background(), "hermes",
		AgentEntry{Path: path, Command: "hermes"})
	if verdict != builtinProbeOK {
		t.Fatalf("verdict = %v (reason %q), want OK", verdict, reason)
	}
	if version != "0.22.5" {
		t.Fatalf("version = %q, want 0.22.5", version)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "acp --version" {
			t.Fatalf("hermes was invoked as %q; every registration probe must be `acp --version`", line)
		}
	}
}
