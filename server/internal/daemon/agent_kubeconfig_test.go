package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugifyAgentName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Boris", "boris"},
		{"  Boris  ", "boris"},
		{"Doug - CTO", "doug"},
		{"Kael OpenClaw", "kael"},
		{"Maggie - COO", "maggie"},
		{"theo", "theo"},
		{"K9-Bot", "k9-bot"},
		{"!!!", ""},
		{"", ""},
		{"   ", ""},
		{"Émilie", "milie"}, // non-ASCII chars are dropped; remaining ASCII forms the slug
	}
	for _, tc := range cases {
		if got := slugifyAgentName(tc.in); got != tc.want {
			t.Errorf("slugifyAgentName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKubeconfigPathForAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(kubeconfigDirEnv, dir)

	// Drop two files: a regular kubeconfig and a directory with a
	// kubeconfig-shaped name to verify directories are rejected.
	if err := os.WriteFile(filepath.Join(dir, "boris.kubeconfig"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write boris: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "rogue.kubeconfig"), 0o755); err != nil {
		t.Fatalf("mkdir rogue: %v", err)
	}

	cases := []struct {
		name, agent, want string
	}{
		{"exact match", "Boris", filepath.Join(dir, "boris.kubeconfig")},
		{"title suffix ignored", "Boris - CTO", filepath.Join(dir, "boris.kubeconfig")},
		{"missing file returns empty", "Doug - CTO", ""},
		{"directory is not a kubeconfig", "Rogue", ""},
		{"empty name", "", ""},
		{"whitespace-only", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kubeconfigPathForAgent(tc.agent); got != tc.want {
				t.Errorf("kubeconfigPathForAgent(%q) = %q, want %q", tc.agent, got, tc.want)
			}
		})
	}
}

func TestKubeconfigPathForAgent_DefaultDir(t *testing.T) {
	// With env unset and no /etc/multica/kube on a typical CI host, the
	// lookup should silently return "". Don't fail when running on a host
	// that happens to have the file (developer machine).
	t.Setenv(kubeconfigDirEnv, "")
	got := kubeconfigPathForAgent("nonexistent-agent-xyz")
	if got != "" {
		t.Errorf("expected empty for unknown agent under default dir, got %q", got)
	}
}
