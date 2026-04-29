package agent

import (
	"errors"
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input   string
		want    semver
		wantErr bool
	}{
		{"2.0.0", semver{2, 0, 0}, false},
		{"v2.1.100", semver{2, 1, 100}, false},
		{"2.1.100 (Claude Code)", semver{2, 1, 100}, false},
		{"codex-cli 0.118.0", semver{0, 118, 0}, false},
		{"1.0.20", semver{1, 0, 20}, false},
		{"invalid", semver{}, true},
		{"", semver{}, true},
	}
	for _, tt := range tests {
		got, err := parseSemver(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSemver(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSemverLessThan(t *testing.T) {
	tests := []struct {
		a, b semver
		want bool
	}{
		{semver{1, 0, 0}, semver{2, 0, 0}, true},
		{semver{2, 0, 0}, semver{1, 0, 0}, false},
		{semver{2, 0, 0}, semver{2, 1, 0}, true},
		{semver{2, 1, 0}, semver{2, 0, 0}, false},
		{semver{2, 1, 12}, semver{2, 1, 13}, true},
		{semver{2, 1, 13}, semver{2, 1, 12}, false},
		{semver{2, 0, 0}, semver{2, 0, 0}, false},
	}
	for _, tt := range tests {
		got := tt.a.lessThan(tt.b)
		if got != tt.want {
			t.Errorf("%v.lessThan(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCheckMinCLIVersion(t *testing.T) {
	tests := []struct {
		detected string
		wantErr  error
	}{
		{"0.2.20", nil},
		{"0.2.21", nil},
		{"1.0.0", nil},
		{"dev", nil},              // dev builds always pass
		{"0.2.19", ErrCLIVersionTooOld},
		{"0.1.0", ErrCLIVersionTooOld},
		{"", ErrCLIVersionMissing},
		{"invalid", ErrCLIVersionMissing},
	}
	for _, tt := range tests {
		err := CheckMinCLIVersion(tt.detected)
		if tt.wantErr == nil && err != nil {
			t.Errorf("CheckMinCLIVersion(%q) unexpected error: %v", tt.detected, err)
		} else if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
			t.Errorf("CheckMinCLIVersion(%q) = %v, want %v", tt.detected, err, tt.wantErr)
		}
	}
}

func TestCheckMinVersion(t *testing.T) {
	tests := []struct {
		agentType string
		version   string
		wantErr   bool
	}{
		{"claude", "2.0.0", false},
		{"claude", "2.1.100", false},
		{"claude", "2.1.100 (Claude Code)", false},
		{"claude", "v2.0.0", false},
		{"claude", "1.0.128", true},
		{"claude", "1.9.99", true},
		{"claude", "invalid", true},
		{"codex", "codex-cli 0.118.0", false},
		{"codex", "codex-cli 0.100.0", false},
		{"codex", "codex-cli 0.99.0", true},
		{"codex", "codex-cli 0.50.0", true},
		{"unknown", "1.0.0", false},
	}
	for _, tt := range tests {
		err := CheckMinVersion(tt.agentType, tt.version)
		if (err != nil) != tt.wantErr {
			t.Errorf("CheckMinVersion(%q, %q) error = %v, wantErr %v", tt.agentType, tt.version, err, tt.wantErr)
		}
	}
}
