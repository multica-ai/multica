//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCodexInitializeParentDeadlineDoesNotPersistOpaqueEnv is the other half of
// the parent-context pair: codex.go treats a parent DEADLINE and a parent
// CANCELLATION as one kind of ending (`contextEnded` in the initialize-failure
// path, codex.go), and both must keep untrusted child stderr out of the Result —
// that stderr can echo opaque Config.Env/auth values which pattern sanitization
// cannot recognize.
//
// Not a transport-timeout test, and it must not be deleted as one: the deadline
// here is the CALLER's own budget, and it is the only thing that exercises the
// DeadlineExceeded arm of `errors.Is(err, context.DeadlineExceeded) ||
// errors.Is(err, context.Canceled)`. The sibling cancellation test covers the
// Canceled arm only.
//
// The fake writes the secret to stderr before reading the handshake, so the
// redaction check never depends on racing our own initialize write. The budget
// is sized against measurement: reaching the app-server's first line costs
// ~430-465ms here (version probe plus launch), so 2s keeps ~4x headroom, and
// the ready-file guard below fails loudly rather than vacuously if a slower
// machine ever misses it.
func TestCodexInitializeParentDeadlineDoesNotPersistOpaqueEnv(t *testing.T) {
	t.Parallel()

	const secret = "opaque-init-parent-deadline-sentinel-8842"
	readyFile := filepath.Join(t.TempDir(), "stderr-written")
	fakePath := writeFakeCodexAppServer(t, ""+
		`echo "$OPAQUE_AUTH_VALUE" >&2`+"\n"+
		`touch "`+readyFile+`"`+"\n"+
		`read line`+"\n"+
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
	// Below the 4s handshake bound, so the parent deadline is what ends the run.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 10 * time.Second, HandshakeTimeout: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatalf("fake app-server never wrote the secret to stderr, so the redaction check would pass vacuously: %v", err)
	}
	persisted := fmt.Sprintf("%+v\n%s", result, logs.String())
	if strings.Contains(persisted, secret) {
		t.Fatalf("opaque env persisted after the parent deadline: %s", persisted)
	}
	if result.Status != "failed" || result.codexInitializeRetrySafe {
		t.Fatalf("parent deadline must fail without initialize retry: %+v", result)
	}
}

func TestCodexInitializeParentCancellationDoesNotPersistOpaqueEnv(t *testing.T) {
	t.Parallel()

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 10 * time.Second, HandshakeTimeout: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
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
}
