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

func TestExtractVersionLine(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "bare semver",
			raw:  "0.42.0\n",
			want: "0.42.0",
		},
		{
			name: "claude full string preserved",
			raw:  "2.1.5 (Claude Code)\n",
			want: "2.1.5 (Claude Code)",
		},
		{
			name: "codex prefix preserved",
			raw:  "codex-cli 0.118.0\n",
			want: "codex-cli 0.118.0",
		},
		// Reproduces #2516: gemini's Windows shim emits `chcp` output to stdout
		// before the real version. The chcp line has no dotted-number form,
		// so the semver scan skips it and picks up "0.42.0" from the next line.
		{
			name: "windows chcp prefix before version",
			raw:  "Active code page: 65001\n0.42.0\n",
			want: "0.42.0",
		},
		{
			name: "windows chcp prefix CRLF",
			raw:  "Active code page: 65001\r\n0.42.0\r\n",
			want: "0.42.0",
		},
		{
			name: "leading blank lines",
			raw:  "\n\n  0.42.0\n",
			want: "0.42.0",
		},
		{
			name: "non-semver output falls back to trimmed raw",
			raw:  "  some-build-id  \n",
			want: "some-build-id",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractVersionLine(tt.raw); got != tt.want {
				t.Errorf("extractVersionLine(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
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
