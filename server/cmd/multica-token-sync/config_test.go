package main

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if !cfg.Once {
		t.Errorf("Once default = %v, want true", cfg.Once)
	}
	if cfg.Interval != 30*time.Minute {
		t.Errorf("Interval default = %v", cfg.Interval)
	}
	if cfg.Namespace != "multica" {
		t.Errorf("Namespace default = %q", cfg.Namespace)
	}
	if cfg.SecretName != "multica-claude-oauth-broker" {
		t.Errorf("SecretName default = %q", cfg.SecretName)
	}
	if cfg.KeychainService != "Claude Code-credentials" {
		t.Errorf("KeychainService default = %q", cfg.KeychainService)
	}
	if cfg.KeychainAccount == "" {
		t.Error("KeychainAccount should default to $USER, got empty")
	}
	if cfg.DryRun {
		t.Error("DryRun must default to false")
	}
}

func TestParseFlags_DaemonMode(t *testing.T) {
	cfg, err := ParseFlags([]string{"--interval", "5m"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.Once {
		t.Error("--interval must disable --once")
	}
	if cfg.Interval != 5*time.Minute {
		t.Errorf("Interval = %v", cfg.Interval)
	}
}

func TestParseFlags_IntervalTooShort(t *testing.T) {
	if _, err := ParseFlags([]string{"--interval", "1s"}); err == nil {
		t.Error("expected error for --interval < 10s")
	}
}

func TestParseFlags_ExplicitAccountOverridesDefault(t *testing.T) {
	cfg, err := ParseFlags([]string{"--keychain-account", "someone"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.KeychainAccount != "someone" {
		t.Errorf("KeychainAccount = %q", cfg.KeychainAccount)
	}
}
