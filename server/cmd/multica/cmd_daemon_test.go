package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDaemonInstancePaths_UseExplicitConfigDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "envs", "local", "config.json")

	dir := daemonDirForInstance("", configPath)
	if dir != filepath.Dir(configPath) {
		t.Fatalf("daemonDirForInstance() = %q, want %q", dir, filepath.Dir(configPath))
	}
	if got := daemonPIDPathForInstance("", configPath); got != filepath.Join(filepath.Dir(configPath), "daemon.pid") {
		t.Fatalf("daemonPIDPathForInstance() = %q", got)
	}
	if got := daemonLogPathForInstance("", configPath); got != filepath.Join(filepath.Dir(configPath), "daemon.log") {
		t.Fatalf("daemonLogPathForInstance() = %q", got)
	}
}

func TestHealthPortForInstance_IsolatedByConfigPath(t *testing.T) {
	configA := filepath.Join(t.TempDir(), "envs", "a", "config.json")
	configB := filepath.Join(t.TempDir(), "envs", "b", "config.json")

	portA := healthPortForInstance("", configA)
	portB := healthPortForInstance("", configB)
	if portA == portB {
		t.Fatalf("expected distinct ports for distinct config paths, got %d", portA)
	}
}

func TestBuildDaemonStartArgs_ForwardsConfigAndProfile(t *testing.T) {
	cmd := testCmd()
	configPath := filepath.Join(t.TempDir(), "envs", "local", "config.json")
	if err := cmd.Flags().Set("config", configPath); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if err := cmd.Flags().Set("profile", "dev"); err != nil {
		t.Fatalf("set profile: %v", err)
	}

	got := buildDaemonStartArgs(cmd)
	want := []string{"daemon", "start", "--foreground", "--config", configPath, "--profile", "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDaemonStartArgs() = %v, want %v", got, want)
	}
}
