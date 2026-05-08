package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadConfigDiscoversQwenPaw(t *testing.T) {
	dir := t.TempDir()
	name := "qwenpaw"
	script := []byte("#!/usr/bin/env sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		name += ".bat"
		script = []byte("@echo off\r\nexit /b 0\r\n")
	}
	fakeQwenPaw := filepath.Join(dir, name)
	if err := os.WriteFile(fakeQwenPaw, script, 0o755); err != nil {
		t.Fatalf("write fake qwenpaw: %v", err)
	}

	t.Setenv("MULTICA_QWENPAW_PATH", fakeQwenPaw)
	t.Setenv("MULTICA_QWENPAW_MODEL", "dashscope:qwen3.6-plus")
	t.Setenv("MULTICA_QWENPAW_BYPASS_PERMISSIONS", "false")

	cfg, err := LoadConfig(Overrides{DaemonID: "test-daemon"})
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	entry, ok := cfg.Agents["qwenpaw"]
	if !ok {
		t.Fatalf("qwenpaw agent not discovered: %#v", cfg.Agents)
	}
	if entry.Path != fakeQwenPaw {
		t.Fatalf("qwenpaw path = %q, want %q", entry.Path, fakeQwenPaw)
	}
	if entry.Model != "dashscope:qwen3.6-plus" {
		t.Fatalf("qwenpaw model = %q", entry.Model)
	}
}