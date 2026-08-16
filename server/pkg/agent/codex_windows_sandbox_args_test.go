package agent

import (
	"reflect"
	"testing"
)

func TestNormalizeCodexWindowsSandboxCustomArgs(t *testing.T) {
	t.Parallel()

	const sandbox = `windows.sandbox="unelevated"`
	tests := []struct {
		name              string
		goos              string
		lowerPriorityOwns bool
		customArgs        []string
		want              []string
	}{
		{
			name:       "windows prepends two canonical managed tokens",
			goos:       "windows",
			customArgs: []string{"--profile", "research"},
			want:       []string{"-c", sandbox, "--profile", "research"},
		},
		{
			name:       "managed prefix is idempotent",
			goos:       "windows",
			customArgs: []string{"-c", sandbox, "--profile", "research"},
			want:       []string{"-c", sandbox, "--profile", "research"},
		},
		{
			name:       "existing two-token override wins",
			goos:       "windows",
			customArgs: []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
			want:       []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name:              "lower priority owner removes a persisted managed prefix",
			goos:              "windows",
			lowerPriorityOwns: true,
			customArgs:        []string{"-c", sandbox, "--profile", "research"},
			want:              []string{"--profile", "research"},
		},
		{
			name:       "explicit custom override removes a persisted managed prefix",
			goos:       "windows",
			customArgs: []string{"-c", sandbox, "-c", `windows.sandbox="elevated"`, "--profile", "research"},
			want:       []string{"-c", `windows.sandbox="elevated"`, "--profile", "research"},
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
		{
			name:       "non-windows removes a stale managed prefix",
			goos:       "linux",
			customArgs: []string{"-c", sandbox, "--profile", "research"},
			want:       []string{"--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeCodexWindowsSandboxCustomArgs(tt.goos, tt.lowerPriorityOwns, tt.customArgs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeCodexWindowsSandboxCustomArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCodexArgsAppliesWindowsSandboxAtSpawnSeam(t *testing.T) {
	t.Parallel()

	got := buildCodexArgs(ExecOptions{
		GOOS:       "windows",
		CustomArgs: []string{"--profile", "research"},
	}, nil)
	want := []string{
		"app-server",
		"--listen",
		"stdio://",
		"-c",
		`windows.sandbox="unelevated"`,
		"--profile",
		"research",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() = %v, want %v", got, want)
	}
}

func TestBuildCodexArgsWindowsSandboxPrecedenceAndPlatformTransitions(t *testing.T) {
	t.Parallel()

	const managed = `windows.sandbox="unelevated"`
	tests := []struct {
		name string
		opts ExecOptions
		want []string
	}{
		{
			name: "runtime argument owns setting without duplicate",
			opts: ExecOptions{
				GOOS:       "windows",
				ExtraArgs:  []string{"-c", `windows.sandbox="elevated"`},
				CustomArgs: []string{"-c", managed, "--profile", "research"},
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "explicit per-agent override owns setting without managed prefix",
			opts: ExecOptions{
				GOOS:       "windows",
				CustomArgs: []string{"-c", managed, "-c", `windows.sandbox="elevated"`, "--profile", "research"},
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "non-windows spawn removes stale managed prefix",
			opts: ExecOptions{
				GOOS:       "linux",
				CustomArgs: []string{"-c", managed, "--profile", "research"},
			},
			want: []string{"app-server", "--listen", "stdio://", "--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := buildCodexArgs(tt.opts, nil); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildCodexArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
