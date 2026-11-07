package agentcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChannelFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "", want: ""},
		{path: "/usr/lib/node_modules/@openai/codex/bin/codex", want: ChannelNPM},
		{path: `/Users/a/AppData/Roaming/npm/node_modules/@anthropic-ai/claude-code/bin/claude`, want: ChannelNPM},
		{path: "/opt/homebrew/Cellar/codex/0.99.0/bin/codex", want: ChannelBrew},
		{path: "/home/linuxbrew/.linuxbrew/bin/claude", want: ChannelBrew},
		{path: "/home/user/.grok/bin/grok", want: ""},
		{path: "/usr/local/bin/codex", want: ""},
	}
	for _, tc := range cases {
		if got := ChannelFromPath(tc.path); got != tc.want {
			t.Errorf("ChannelFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestHasNPMPrefix(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, ".local")
	if err := os.MkdirAll(filepath.Join(prefix, "lib", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(prefix, "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasNPMPrefix(bin, os.Stat) {
		t.Fatalf("HasNPMPrefix(%s) = false, want the bootstrap ~/.local prefix layout", bin)
	}

	win := filepath.Join(root, "Roaming", "npm", "claude.cmd")
	if err := os.MkdirAll(filepath.Join(root, "Roaming", "npm", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasNPMPrefix(win, os.Stat) {
		t.Fatalf("HasNPMPrefix(%s) = false, want the Windows npm layout", win)
	}
	if HasNPMPrefix(filepath.Join(root, ".grok", "bin", "grok"), os.Stat) {
		t.Fatal("a grok binary directory must not look like npm")
	}
}
