//go:build linux || darwin

package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Inject the reported kernel error; this does not reproduce macOS 12 itself.
func TestPiCleanupPreflightBeforeStart(t *testing.T) {
	for _, provider := range []string{"pi", "omp"} {
		for _, supported := range []bool{false, true} {
			name := provider + "/unsupported"
			if supported {
				name = provider + "/supported"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				marker := filepath.Join(dir, "started")
				fake := filepath.Join(dir, "fake-runtime")
				writeTestExecutable(t, fake, []byte("#!/bin/sh\nprintf started > \"$PREFLIGHT_MARKER\"\n"+strings.TrimPrefix(piEventStreamScript([]string{`{"type":"agent_end","messages":[]}`}), "#!/bin/sh\n")))
				backend, err := ResolveBackend(provider, Config{ExecutablePath: fake, Env: map[string]string{"PREFLIGHT_MARKER": marker}, Logger: slog.Default()})
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				backend.(*piBackend).cleanupPreflight = func() error {
					calls++
					return probePiIdentitySignalling(func(int, uint32, syscall.Signal) error {
						if supported {
							return syscall.ESRCH
						}
						return syscall.EINVAL
					})
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				sessionPath := filepath.Join(dir, "session.jsonl")
				if provider == "omp" && supported {
					if err := os.WriteFile(sessionPath, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				session, err := backend.Execute(ctx, "test", ExecOptions{Cwd: dir, ResumeSessionID: sessionPath})
				if supported {
					if err != nil {
						t.Fatal(err)
					}
					go func() {
						for range session.Messages {
						}
					}()
					result := waitPiResult(t, session, 5*time.Second)
					if result.Status != "completed" {
						t.Fatalf("result=%+v", result)
					}
					if _, err := os.Stat(marker); err != nil {
						t.Fatalf("fixture did not start: %v", err)
					}
				} else {
					if session != nil || !errors.Is(err, syscall.EINVAL) || !strings.Contains(err.Error(), provider+" process cleanup unavailable before start") {
						t.Fatalf("expected actionable startup error, got session=%v error=%v", session != nil, err)
					}
					for _, path := range []string{marker, sessionPath} {
						if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("startup had side effects at %s: %v", path, err)
						}
					}
				}
				if calls != 1 {
					t.Fatalf("preflight calls=%d, want 1", calls)
				}
			})
		}
	}
}
