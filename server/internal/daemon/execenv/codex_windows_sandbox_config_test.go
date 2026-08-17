package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexWindowsSandboxConfigOwns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content *string
		want    bool
	}{
		{name: "missing config", want: false},
		{name: "unrelated config", content: stringPtr(`model = "gpt-5"`), want: false},
		{name: "explicit elevated", content: stringPtr("[windows]\nsandbox = \"elevated\"\n"), want: true},
		{name: "explicit empty fails closed", content: stringPtr("[windows]\nsandbox = \"\"\n"), want: true},
		{name: "malformed config fails closed", content: stringPtr("[windows\n"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			configFile := filepath.Join(t.TempDir(), "config.toml")
			if tt.content != nil {
				if err := os.WriteFile(configFile, []byte(*tt.content), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			if got := CodexWindowsSandboxConfigOwns(configFile); got != tt.want {
				t.Fatalf("CodexWindowsSandboxConfigOwns() = %v, want %v", got, tt.want)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }
