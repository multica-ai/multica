package agent

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCommandExecAppliesTaskScopePrefix(t *testing.T) {
	cfg := Config{
		LaunchPrefix:  []string{"tenant"},
		CommandPrefix: []string{"systemd-run", "--user", "--scope", "--"},
	}
	cmd := cfg.commandAt("codex").exec(context.Background(), "app-server")

	if filepath.Base(cmd.Path) != "systemd-run" {
		t.Fatalf("path = %q, want systemd-run", cmd.Path)
	}
	want := []string{"systemd-run", "--user", "--scope", "--", "codex", "tenant", "app-server"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, want)
	}
}

func TestCommandExecWithoutTaskScopeIsUnchanged(t *testing.T) {
	cmd := (Config{LaunchPrefix: []string{"tenant"}}).commandAt("codex").exec(context.Background(), "app-server")
	want := []string{"codex", "tenant", "app-server"}
	if filepath.Base(cmd.Path) != "codex" || !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command = %q %#v, want codex %#v", cmd.Path, cmd.Args, want)
	}
}

func TestTaskCommandAppliesScopeWithoutProviderLaunchPrefix(t *testing.T) {
	cfg := Config{
		LaunchPrefix:  []string{"must-not-leak"},
		CommandPrefix: []string{"systemd-run", "--user", "--scope", "--"},
	}
	cmd := cfg.taskCommandAt("/bin/sh").exec(context.Background(), "-c", "true")
	want := []string{"systemd-run", "--user", "--scope", "--", "/bin/sh", "-c", "true"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, want)
	}
}
