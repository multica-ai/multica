package agent

import (
	"reflect"
	"testing"
)

func TestEnsureCodexWindowsSandboxCustomArgs(t *testing.T) {
	t.Parallel()

	const sandbox = `windows.sandbox="unelevated"`
	tests := []struct {
		name       string
		goos       string
		extraArgs  []string
		customArgs []string
		want       []string
	}{
		{
			name:       "windows appends two canonical tokens",
			goos:       "windows",
			customArgs: []string{"--profile", "research"},
			want:       []string{"--profile", "research", "-c", sandbox},
		},
		{
			name:       "existing two-token override wins",
			goos:       "windows",
			customArgs: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
			want:       []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name:       "existing inline lower-priority override prevents duplicate",
			goos:       "windows",
			extraArgs:  []string{`--config=windows.sandbox="elevated"`},
			customArgs: []string{"--profile", "research"},
			want:       []string{"--profile", "research"},
		},
		{
			name:       "shell-quoted override is detected through launch normalization",
			goos:       "windows",
			customArgs: []string{"'-c'", `'windows.sandbox="unelevated"'`},
			want:       []string{"'-c'", `'windows.sandbox="unelevated"'`},
		},
		{
			name:       "non-windows remains unchanged",
			goos:       "linux",
			customArgs: []string{"--profile", "research"},
			want:       []string{"--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EnsureCodexWindowsSandboxCustomArgs(tt.goos, tt.extraArgs, tt.customArgs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EnsureCodexWindowsSandboxCustomArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCodexArgsIncludesWindowsSandboxTokens(t *testing.T) {
	t.Parallel()

	customArgs := EnsureCodexWindowsSandboxCustomArgs(
		"windows",
		nil,
		[]string{"--profile", "research"},
	)
	got := buildCodexArgs(ExecOptions{CustomArgs: customArgs}, nil)
	want := []string{
		"app-server",
		"--listen",
		"stdio://",
		"--profile",
		"research",
		"-c",
		`windows.sandbox="unelevated"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() = %v, want %v", got, want)
	}
}
