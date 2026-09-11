//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCodexThreadStartTimeoutReapsDetachedStdioDescendant(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "descendant.pid")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		// Spawned before initialize answers, so the descendant exists however
		// soon the thread/start bound fires.
		`sleep 30 >/dev/null 2>&1 & echo $! > "`+pidFile+`"`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`sleep 1.2`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-late"}}}'`+"\n"+
		`read line`+"\n")

	var logs strings.Builder
	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 4 * time.Second, HandshakeTimeout: time.Second, ThreadHandshakeTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected thread/start failure, got %+v", result)
	}
	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, pid)
	failure := findCodexLifecyclePhase(t, parseJSONLogEntries(t, logs.String()), "thread_start_failure")
	if failure["reaped"] != true || failure["cleanup_confirmed"] != true {
		t.Fatalf("process-tree cleanup not confirmed: %v", failure)
	}
}

// TestCodexInitializeTimeoutReapsDescendantWithoutPersistingOpaqueEnv drives one
// initialize timeout and checks both what the kill must reach (a descendant
// with detached stdio) and what the failure must not carry (stderr that echoed
// an opaque Config.Env value).
func TestCodexInitializeTimeoutReapsDescendantWithoutPersistingOpaqueEnv(t *testing.T) {
	// Parallel-safe: no package-level override is touched, and the serial tests
	// that assert on the shared launch counters never overlap parallel ones.
	t.Parallel()

	const secret = "opaque-init-auth-sentinel-7319"
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "descendant.pid")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`sleep 30 >/dev/null 2>&1 & echo $! > "`+pidFile+`"`+"\n"+
		`sleep 1.2`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n")

	var logs strings.Builder
	backendRaw, err := New("codex", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"OPAQUE_AUTH_VALUE": secret},
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := backendRaw.(*codexBackend)
	// initialize never answers in time, but the fake must write stderr and spawn
	// its descendant before the kill, and forked sh startup under load can take
	// well past a few hundred ms.
	session, err := backend.executeOnce(context.Background(), "prompt", ExecOptions{Timeout: 4 * time.Second, HandshakeTimeout: time.Second}, 1)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, CodexHandshakeTimeoutMarker) || !strings.Contains(result.Error, "initialize") {
		t.Fatalf("expected initialize timeout failure, got %+v", result)
	}
	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, pid)
	if strings.Contains(result.Error, secret) {
		t.Fatalf("opaque env persisted in Result.Error: %q", result.Error)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("opaque env persisted in lifecycle logs: %s", logs.String())
	}
	failure := findCodexLifecyclePhase(t, parseJSONLogEntries(t, logs.String()), "initialize_failure")
	if failure["cleanup_confirmed"] != true || failure["retry_safe"] != true {
		t.Fatalf("process-tree cleanup/retry gate not confirmed: %v", failure)
	}
}

func TestCodexInitializeParentContextDoesNotPersistOpaqueEnv(t *testing.T) {
	// Parallel-safe for the same reason as the initialize-timeout test above.
	t.Parallel()

	for _, tc := range []struct {
		name      string
		newCtx    func() (context.Context, context.CancelFunc)
		cancelNow bool
	}{
		{
			// The fake must echo the secret before this deadline for the
			// redaction check to mean anything; the marker check below
			// enforces that.
			name: "deadline before handshake timeout",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), time.Second)
			},
		},
		{
			name: "parent cancellation",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			cancelNow: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "opaque-init-parent-context-sentinel-8841"
			readyFile := filepath.Join(t.TempDir(), "stderr-written")
			fakePath := writeFakeCodexAppServer(t, ""+
				`read line`+"\n"+
				`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
				`touch "`+readyFile+`"`+"\n"+
				`sleep 5`+"\n")

			var logs strings.Builder
			backend, err := New("codex", Config{
				ExecutablePath: fakePath,
				Env:            map[string]string{"OPAQUE_AUTH_VALUE": secret},
				Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tc.newCtx()
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 10 * time.Second, HandshakeTimeout: 4 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			if tc.cancelNow {
				deadline := time.Now().Add(3 * time.Second)
				for {
					if _, err := os.Stat(readyFile); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("fake app-server did not write stderr before cancellation")
					}
					time.Sleep(20 * time.Millisecond)
				}
				cancel()
			}
			result := <-session.Result
			if _, err := os.Stat(readyFile); err != nil {
				t.Fatalf("fake app-server never wrote the secret to stderr, so the redaction check would pass vacuously: %v", err)
			}
			persisted := fmt.Sprintf("%+v\n%s", result, logs.String())
			if strings.Contains(persisted, secret) {
				t.Fatalf("opaque env persisted after parent context ended: %s", persisted)
			}
			if result.Status != "failed" || result.codexInitializeRetrySafe {
				t.Fatalf("parent context must fail without initialize retry: %+v", result)
			}
		})
	}
}
