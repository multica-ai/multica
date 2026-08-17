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
		managed           bool
		lowerPriorityOwns bool
		customArgs        []string
		want              []string
		wantManaged       bool
	}{
		{
			name:        "windows prepends two canonical managed tokens",
			goos:        "windows",
			customArgs:  []string{"--profile", "research"},
			want:        []string{"-c", sandbox, "--profile", "research"},
			wantManaged: true,
		},
		{
			name:        "managed prefix is idempotent",
			goos:        "windows",
			managed:     true,
			customArgs:  []string{"-c", sandbox, "--profile", "research"},
			want:        []string{"-c", sandbox, "--profile", "research"},
			wantManaged: true,
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
			managed:           true,
			lowerPriorityOwns: true,
			customArgs:        []string{"-c", sandbox, "--profile", "research"},
			want:              []string{"--profile", "research"},
		},
		{
			name:       "explicit custom override removes a persisted managed prefix",
			goos:       "windows",
			managed:    true,
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
			name:       "non-windows removes a proven managed prefix",
			goos:       "linux",
			managed:    true,
			customArgs: []string{"-c", sandbox, "--profile", "research"},
			want:       []string{"--profile", "research"},
		},
		{
			name:       "non-windows preserves an identical user-owned pair",
			goos:       "linux",
			customArgs: []string{"-c", sandbox, "--profile", "research"},
			want:       []string{"-c", sandbox, "--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, gotManaged := NormalizeCodexWindowsSandboxCustomArgs(tt.goos, tt.managed, tt.lowerPriorityOwns, tt.customArgs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeCodexWindowsSandboxCustomArgs() = %v, want %v", got, tt.want)
			}
			if gotManaged != tt.wantManaged {
				t.Fatalf("NormalizeCodexWindowsSandboxCustomArgs() managed = %v, want %v", gotManaged, tt.wantManaged)
			}
		})
	}
}

func TestLastCodexWindowsSandboxOverrideUsesCodexLastWinsSemantics(t *testing.T) {
	t.Parallel()

	got, ok := LastCodexWindowsSandboxOverride([]string{
		"'--config=windows.sandbox=\"elevated\"'",
		"--profile",
		"research",
		"-c",
		`windows . sandbox = "unelevated"`,
	})
	if !ok {
		t.Fatal("LastCodexWindowsSandboxOverride() did not find the override")
	}
	if got != ` "unelevated"` {
		t.Fatalf("LastCodexWindowsSandboxOverride() = %q, want %q", got, ` "unelevated"`)
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
				GOOS:                            "windows",
				ExtraArgs:                       []string{"-c", `windows.sandbox="elevated"`},
				CustomArgs:                      []string{"-c", managed, "--profile", "research"},
				IsCodexWindowsSandboxArgManaged: true,
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "explicit per-agent override owns setting without managed prefix",
			opts: ExecOptions{
				GOOS:                            "windows",
				CustomArgs:                      []string{"-c", managed, "-c", `windows.sandbox="elevated"`, "--profile", "research"},
				IsCodexWindowsSandboxArgManaged: true,
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox="elevated"`, "--profile", "research"},
		},
		{
			name: "present empty override remains explicit without a managed duplicate",
			opts: ExecOptions{
				GOOS:       "windows",
				CustomArgs: []string{"-c", `windows.sandbox=""`, "--profile", "research"},
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox=""`, "--profile", "research"},
		},
		{
			name: "non-windows spawn removes stale managed prefix",
			opts: ExecOptions{
				GOOS:                            "linux",
				CustomArgs:                      []string{"-c", managed, "--profile", "research"},
				IsCodexWindowsSandboxArgManaged: true,
			},
			want: []string{"app-server", "--listen", "stdio://", "--profile", "research"},
		},
		{
			name: "explicit canonical custom arg beats lower priority runtime setting",
			opts: ExecOptions{
				GOOS:       "windows",
				ExtraArgs:  []string{"-c", `windows.sandbox="elevated"`},
				CustomArgs: []string{"-c", managed, "--profile", "research"},
			},
			want: []string{"app-server", "--listen", "stdio://", "-c", `windows.sandbox="elevated"`, "-c", managed, "--profile", "research"},
		},
		{
			name: "copied config owns setting over managed default",
			opts: ExecOptions{
				GOOS:                            "windows",
				CustomArgs:                      []string{"-c", managed, "--profile", "research"},
				IsCodexWindowsSandboxArgManaged: true,
				CodexWindowsSandboxConfigOwns:   true,
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
