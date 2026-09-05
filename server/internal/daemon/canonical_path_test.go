//go:build !windows

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsNameDispatchingAgentShim_RequiresExactDispatcherName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "Volta", path: filepath.Join("manager", "volta-shim"), want: true},
		{name: "Vite Plus", path: filepath.Join("manager", "vp"), want: true},
		{name: "case insensitive", path: filepath.Join("manager", "VP"), want: true},
		{name: "short name prefix", path: filepath.Join("manager", "vpn"), want: false},
		{name: "short name word", path: filepath.Join("manager", "vproxy"), want: false},
		{name: "extension is not stripped", path: filepath.Join("manager", "volta-shim.exe"), want: false},
		{name: "ordinary version target", path: filepath.Join("manager", "claude-2.1.216"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNameDispatchingAgentShim(tt.path); got != tt.want {
				t.Fatalf("isNameDispatchingAgentShim(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveMiseManagedExecutable_RejectsManagerAsTarget(t *testing.T) {
	mise := filepath.Join(t.TempDir(), "mise")
	script := "#!/bin/sh\nprintf '%s\\n' \"$MULTICA_TEST_MISE_SELF\"\n"
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mise: %v", err)
	}
	t.Setenv("MULTICA_TEST_MISE_SELF", mise)

	_, err := resolveMiseManagedExecutable(context.Background(), mise, "claude")
	if err == nil || !strings.Contains(err.Error(), "manager executable") {
		t.Fatalf("resolve manager target error = %v, want fail-closed diagnosis", err)
	}
}

func TestResolveMiseManagedExecutable_RejectsInvalidEnvironment(t *testing.T) {
	target := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write managed target: %v", err)
	}
	mise := filepath.Join(t.TempDir(), "mise")
	script := `#!/bin/sh
case "$1" in
  which) printf '%s\n' "$MULTICA_TEST_MISE_TARGET" ;;
  env) printf '{"JAVA_HOME":"/java"}\n' ;;
esac
`
	if err := os.WriteFile(mise, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mise: %v", err)
	}
	t.Setenv("MULTICA_TEST_MISE_TARGET", target)

	_, err := resolveMiseManagedExecutable(context.Background(), mise, "claude")
	if err == nil || !strings.Contains(err.Error(), "returned no PATH") {
		t.Fatalf("resolve target without mise PATH error = %v, want fail-closed diagnosis", err)
	}
}

func TestSanitizeMiseResolvedEnv_PreservesToolsetWithoutDaemonOwnedValues(t *testing.T) {
	got, err := sanitizeMiseResolvedEnv(map[string]string{
		"PATH":          "/mise/node/bin:/usr/bin:/bin",
		"JAVA_HOME":     "/mise/java",
		"HOME":          "/mise/home",
		"MULTICA_TOKEN": "not-a-task-token",
	})
	if err != nil {
		t.Fatalf("sanitize mise environment: %v", err)
	}
	if got["PATH"] != "/mise/node/bin:/usr/bin:/bin" || got["JAVA_HOME"] != "/mise/java" {
		t.Fatalf("sanitized mise environment lost tool values: %v", got)
	}
	if _, ok := got["HOME"]; ok {
		t.Fatal("sanitized mise environment retained daemon-owned HOME")
	}
	if _, ok := got["MULTICA_TOKEN"]; ok {
		t.Fatal("sanitized mise environment retained task-owned MULTICA_TOKEN")
	}
}

func TestResolveMiseManagedExecutable_TimesOut(t *testing.T) {
	mise := filepath.Join(t.TempDir(), "mise")
	if err := os.WriteFile(mise, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatalf("write fake mise: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := resolveMiseManagedExecutable(ctx, mise, "claude")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolveMiseManagedExecutable error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timed-out mise which took %v, want at most 1s", elapsed)
	}
}
