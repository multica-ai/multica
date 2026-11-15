package agent

import (
	"path/filepath"
	"testing"
)

func TestClaudeConfigDir(t *testing.T) {
	home, inheritedConfig, taskHome, taskConfig, cwd := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tt := range []struct {
		name      string
		inherited string
		overrides map[string]string
		want      string
	}{
		{name: "inherited config", inherited: inheritedConfig, want: inheritedConfig},
		{name: "task config wins", inherited: inheritedConfig, overrides: map[string]string{"CLAUDE_CONFIG_DIR": taskConfig}, want: taskConfig},
		{name: "empty task config uses home", inherited: inheritedConfig, overrides: map[string]string{"CLAUDE_CONFIG_DIR": ""}, want: filepath.Join(home, ".claude")},
		{name: "inherited home", want: filepath.Join(home, ".claude")},
		{name: "child home", overrides: map[string]string{"HOME": taskHome, "USERPROFILE": taskHome}, want: filepath.Join(taskHome, ".claude")},
		{name: "relative config", overrides: map[string]string{"CLAUDE_CONFIG_DIR": "../claude-config"}, want: filepath.Join(filepath.Dir(cwd), "claude-config")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", tt.inherited)
			got, err := ClaudeConfigDir(tt.overrides, cwd)
			if err != nil || got != tt.want {
				t.Fatalf("ClaudeConfigDir() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
