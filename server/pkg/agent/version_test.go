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
		name    string
		input   string
		wantErr error
	}{
		{"tagged release at minimum", "v0.2.20", nil},
		{"tagged release above minimum", "0.3.1", nil},
		{"tagged release below minimum", "v0.2.15", ErrCLIVersionTooOld},
		{"empty string", "", ErrCLIVersionMissing},
		{"unparsable", "not-a-version", ErrCLIVersionMissing},
		{"git-describe dev build past old tag", "v0.2.15-235-gdaf0e935", nil},
		{"git-describe dirty dev build", "v0.2.15-235-gdaf0e935-dirty", nil},
		{"git-describe dev build past current tag", "v0.2.20-3-gabc1234", nil},
	}
	for _, tt := range tests {
		err := CheckMinCLIVersion(tt.input)
		if tt.wantErr == nil && err != nil {
			t.Errorf("%s: CheckMinCLIVersion(%q) = %v, want nil", tt.name, tt.input, err)
		}
		if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: CheckMinCLIVersion(%q) = %v, want %v", tt.name, tt.input, err, tt.wantErr)
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
